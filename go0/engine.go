package revera

// This file holds the phase A executor.
// It scans the subject once, runs every viable automaton path in lockstep, and returns the selected match start and end.
// Thread payloads hold the match start and the minimal repetition counters.
// Captures are the job of phase B.
//
// Merging keeps the best (start, counters) payload per instruction and position.
// Futures from a shared instruction are identical, and counters only grow.
// The merge therefore keeps the selected candidate.

import (
	"fmt"
	"math/bits"
	"slices"
	"strings"

	"revera/locale"
)

// maxElemAhead is the largest character count one transition can consume.
// It must cover the longest collating element of the locale data.
// init checks that the data still fits after a regeneration.
const maxElemAhead = 8

func init() {
	if locale.MaxElementLength() > maxElemAhead {
		panic(fmt.Sprintf("maxElemAhead = %d is smaller than the longest collating element (%d)",
			maxElemAhead, locale.MaxElementLength()))
	}
}

// slotTable holds the payloads for one character boundary.
type slotTable struct {
	stamp  []uint32
	starts []int32
	ctr    []uint32
	active []uint32
	gen    uint32
}

type engineWS struct {
	slots []slotTable
	queue []uint32
	// onq is scratch for compactQueue.
	// It is all false between calls.
	onq     []bool
	bestCtr []uint32
	ctrBuf  []uint32
	zeros   []uint32
	ahead   []rune
	// base keeps generation stamps unique across pooled reuses.
	base uint32
}

func (re *Regexp) getWS() *engineWS {
	if ws, ok := re.pool.Get().(*engineWS); ok {
		return ws
	}
	return &engineWS{}
}

func (re *Regexp) putWS(ws *engineWS) {
	re.pool.Put(ws)
}

// prepare sizes the workspace for this program.
// workspaceHeapBound mirrors this sizing for the resource contract, so the two must stay in step.
func (ws *engineWS) prepare(prog *program, k, ring int) {
	if ws.base > 0xf000_0000 {
		for i := range ws.slots {
			clear(ws.slots[i].stamp)
		}
		ws.base = 0
	}
	if len(ws.slots) < ring {
		ws.slots = append(ws.slots, make([]slotTable, ring-len(ws.slots))...)
	}
	n := len(prog.ins)
	for i := range ring {
		t := &ws.slots[i]
		if len(t.stamp) < n {
			t.stamp = make([]uint32, n)
			t.starts = make([]int32, n)
		}
		if k > 0 && len(t.ctr) < n*k {
			t.ctr = make([]uint32, n*k)
		}
		t.active = t.active[:0]
		t.gen = 0
	}
	if len(ws.onq) < n {
		ws.onq = make([]bool, n)
	}
	if len(ws.bestCtr) < k {
		ws.bestCtr = make([]uint32, k)
		ws.ctrBuf = make([]uint32, k)
		ws.zeros = make([]uint32, k)
	}
	ws.queue = ws.queue[:0]
}

// workspaceHeapBound bounds the bytes prepare and the closure queue can allocate.
// It covers a program of n instructions with k counter slots and the given ring size.
// It uses fixed 64-bit sizes, so the resource contract reports the same figure on every platform.
// It must change together with prepare and the workspace fields.
func workspaceHeapBound(n, k, ring int64) int64 {
	// Each ring slot holds the stamp, start and active arrays, and the counter matrix.
	// The active array grows by append, and doubling can leave twice the needed room.
	heap := cMul(ring, cMul(n, 16+4*k))
	// The queue and its compaction marks.
	heap = cAdd(heap, 8*(queueCompactFactor*n+2)+n)
	// The slot structs, the workspace struct, the three counter vectors, and the lookahead buffer.
	heap = cAdd(heap, cMul(ring, 112)+256)
	return cAdd(heap, 12*k+4*maxElemAhead)
}

// ctrLess compares two counter vectors lexicographically.
// It stays a hand-written loop instead of slices.Compare, so prune and store keep fitting in the inline budget.
// Those two run on every transition.
func ctrLess(a, b []uint32) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// engineResult reports the phase A outcome in byte offsets.
type engineResult struct {
	matched bool
	so, eo  int
}

// phaseA carries the per-call executor state.
// The hot path therefore builds no closures, and it allocates nothing after the workspace warmup.
type phaseA struct {
	re      *Regexp
	prog    *program
	ws      *engineWS
	subject string
	eflags  ExecFlags
	k       int
	ring    int
	nlMode  bool

	ci   int
	pos  int
	bol  bool
	eol  bool
	cur  rune
	size int

	matched bool
	so, eo  int
	bestCtr []uint32
}

func (re *Regexp) runPhaseA(subject string, eflags ExecFlags) engineResult {
	ws := re.getWS()
	result := re.runPhaseAWith(ws, subject, eflags)
	re.putWS(ws)
	return result
}

// runPhaseAWith runs phase A on the given workspace.
func (re *Regexp) runPhaseAWith(ws *engineWS, subject string, eflags ExecFlags) engineResult {
	e := phaseA{
		re:      re,
		prog:    re.prog,
		ws:      ws,
		subject: subject,
		eflags:  eflags,
		k:       re.minSlots,
		ring:    2,
		nlMode:  re.flags&Newline != 0,
	}
	if e.prog.multi {
		e.ring = maxElemAhead + 1
	}
	ws.prepare(e.prog, e.k, e.ring)
	e.bestCtr = ws.bestCtr[:e.k]
	e.run()
	ws.base = e.gen(e.ci) + uint32(e.ring) + 1
	return engineResult{matched: e.matched, so: e.so, eo: e.eo}
}

func (e *phaseA) gen(boundary int) uint32 {
	return e.ws.base + uint32(boundary) + 1
}

// consider records a match candidate.
// It prefers the earliest start, then the smallest counters, then the longest end.
func (e *phaseA) consider(start int32, ctr []uint32, end int) {
	if !e.matched || int(start) < e.so {
		e.matched = true
		e.so = int(start)
		e.eo = end
		copy(e.bestCtr, ctr)
		return
	}
	if int(start) > e.so {
		return
	}
	if e.k > 0 {
		if ctrLess(ctr, e.bestCtr) {
			e.eo = end
			copy(e.bestCtr, ctr)
			return
		}
		if ctrLess(e.bestCtr, ctr) {
			return
		}
	}
	if end > e.eo {
		e.eo = end
	}
}

// prune reports whether a payload can no longer beat the best candidate.
// Counters only grow, so a lexicographically larger vector stays larger.
func (e *phaseA) prune(start int32, ctr []uint32) bool {
	if !e.matched {
		return false
	}
	if int(start) > e.so {
		return true
	}
	if int(start) < e.so || e.k == 0 {
		return false
	}
	return ctrLess(e.bestCtr, ctr)
}

// store merges a payload into a slot.
// It reports whether the payload stayed.
func (e *phaseA) store(t *slotTable, pc uint32, start int32, ctr []uint32) bool {
	if e.prune(start, ctr) {
		return false
	}
	if t.stamp[pc] == t.gen {
		oldStart := t.starts[pc]
		if oldStart < start {
			return false
		}
		if oldStart == start {
			if e.k == 0 {
				return false
			}
			old := t.ctr[int(pc)*e.k : int(pc)*e.k+e.k]
			if !ctrLess(ctr, old) {
				return false
			}
		}
	} else {
		t.stamp[pc] = t.gen
		t.active = append(t.active, pc)
	}
	t.starts[pc] = start
	if e.k > 0 {
		copy(t.ctr[int(pc)*e.k:int(pc)*e.k+e.k], ctr)
	}
	return true
}

// relax merges a payload at the current boundary and queues it for the epsilon closure.
func (e *phaseA) relax(t *slotTable, pc uint32, start int32, ctr []uint32) {
	if e.store(t, pc, start, ctr) {
		e.ws.queue = append(e.ws.queue, pc)
	}
}

// queueCompactFactor sets the compaction threshold as a multiple of the program length.
// The queue can pass that threshold by two pushes before the next pop checks it.
// Append doubling can also leave twice the needed room.
// workspaceHeapBound and the queue test derive their figures from this factor.
const queueCompactFactor = 2

// compactQueue drops duplicate queue entries.
// A duplicate does no harm, because a pop reads the current slot payload.
// One entry per instruction therefore does the same work.
// Dropping the duplicates keeps the queue linear in the program, a bound the resource contract needs.
// The onq marks live only inside this function.
func (e *phaseA) compactQueue() {
	kept := e.ws.queue[:0]
	for _, pc := range e.ws.queue {
		if !e.ws.onq[pc] {
			e.ws.onq[pc] = true
			kept = append(kept, pc)
		}
	}
	for _, pc := range kept {
		e.ws.onq[pc] = false
	}
	e.ws.queue = kept
}

// closure drains the relaxation queue over the epsilon instructions.
// The queue may hold duplicates, and it compacts past twice the program length.
// Its memory therefore stays linear, and the hot push stays a bare append.
func (e *phaseA) closure(t *slotTable) {
	limit := queueCompactFactor * len(e.prog.ins)
	for len(e.ws.queue) > 0 {
		if len(e.ws.queue) > limit {
			e.compactQueue()
		}
		pc := e.ws.queue[len(e.ws.queue)-1]
		e.ws.queue = e.ws.queue[:len(e.ws.queue)-1]
		if t.stamp[pc] != t.gen {
			continue
		}
		start := t.starts[pc]
		var ctr []uint32
		if e.k > 0 {
			ctr = t.ctr[int(pc)*e.k : int(pc)*e.k+e.k]
		}
		ins := &e.prog.ins[pc]
		switch ins.op {
		case iSplit:
			e.relax(t, ins.next, start, ctr)
			e.relax(t, ins.alt, start, ctr)
		case iJmp:
			e.relax(t, ins.next, start, ctr)
		case iBOL:
			if e.bol {
				e.relax(t, ins.next, start, ctr)
			}
		case iEOL:
			if e.eol {
				e.relax(t, ins.next, start, ctr)
			}
		case iMatch:
			e.consider(start, ctr, e.pos)
		}
	}
}

// arrive files a consuming transition into a future boundary slot.
// delta counts the consumed characters.
// Those characters also increment the counters that the instruction's mask selects.
func (e *phaseA) arrive(pc uint32, delta int, start int32, ctr []uint32) {
	future := &e.ws.slots[(e.ci+delta)%e.ring]
	g := e.gen(e.ci + delta)
	if future.gen != g {
		future.gen = g
		future.active = future.active[:0]
	}
	newCtr := ctr
	ins := &e.prog.ins[pc]
	if e.k > 0 && (ins.mask != 0 || len(ins.extra) > 0) {
		buffer := e.ws.ctrBuf[:e.k]
		copy(buffer, ctr)
		for m := ins.mask; m != 0; m &= m - 1 {
			buffer[bits.TrailingZeros64(m)] += uint32(delta)
		}
		for _, slot := range ins.extra {
			buffer[slot] += uint32(delta)
		}
		newCtr = buffer
	}
	e.store(future, ins.next, start, newCtr)
}

// consume advances every consuming instruction over the current character or collating element.
func (e *phaseA) consume(t *slotTable) {
	aheadReady := false
	for _, pc := range t.active {
		if t.stamp[pc] != t.gen {
			continue
		}
		ins := &e.prog.ins[pc]
		start := t.starts[pc]
		var ctr []uint32
		if e.k > 0 {
			ctr = t.ctr[int(pc)*e.k : int(pc)*e.k+e.k]
		}
		switch ins.op {
		case iRune:
			if e.cur == rune(ins.arg) {
				e.arrive(pc, 1, start, ctr)
			}
		case iRuneFold:
			if slices.Contains(e.prog.foldSets[ins.arg], e.cur) {
				e.arrive(pc, 1, start, ctr)
			}
		case iAny:
			if e.re.anyMatches(e.cur) {
				e.arrive(pc, 1, start, ctr)
			}
		case iBracket:
			br := e.prog.brackets[ins.arg]
			if br.matchesOne(e.cur) {
				e.arrive(pc, 1, start, ctr)
			}
			if br.multiLens == 0 {
				continue
			}
			if !aheadReady {
				e.decodeAhead()
				aheadReady = true
			}
			for length := 2; length <= len(e.ws.ahead); length++ {
				if br.multiLens&(1<<length) == 0 {
					continue
				}
				if br.matchesMulti(e.ws.ahead[:length]) {
					e.arrive(pc, length, start, ctr)
				}
			}
		}
	}
}

// decodeAhead fills the lookahead buffer with up to maxElemAhead characters, starting at the current position.
func (e *phaseA) decodeAhead() {
	e.ws.ahead = e.ws.ahead[:0]
	at := e.pos
	for len(e.ws.ahead) < maxElemAhead && at < len(e.subject) {
		r, size := decodeRune(e.subject[at:])
		e.ws.ahead = append(e.ws.ahead, r)
		at += size
	}
}

// scanAhead returns the offset of the next byte in the stop set, or the subject length.
// Stop bytes are ASCII or UTF-8 lead bytes.
// The returned offset is therefore always a boundary that the sequential scan would visit.
func (e *phaseA) scanAhead() int {
	s := e.subject
	if e.prog.scan.single {
		idx := strings.IndexByte(s[e.pos:], e.prog.scan.b)
		if idx < 0 {
			return len(s)
		}
		return e.pos + idx
	}
	stop := &e.prog.scan.stop
	for i := e.pos; i < len(s); i++ {
		if stop[s[i]] {
			return i
		}
	}
	return len(s)
}

// bolAt reports whether a line begins at the current position, for the given preceding character.
func (e *phaseA) bolAt(prev rune) bool {
	return (e.pos == 0 && e.eflags&NotBOL == 0) ||
		(e.nlMode && prev == '\n')
}

// continuationFlags adapts eflags for a search that restarts at pos on a sliced subject.
// It keeps the bolAt rule above.
// A restart is never the true start.
// Only a preceding newline in newline mode leaves a line boundary before it.
func (re *Regexp) continuationFlags(subject string, pos int, eflags ExecFlags) ExecFlags {
	if pos == 0 {
		return eflags
	}
	if re.flags&Newline != 0 && subject[pos-1] == '\n' {
		return eflags &^ NotBOL
	}
	return eflags | NotBOL
}

func (e *phaseA) run() {
	var prev rune = -2
	zeros := e.ws.zeros[:e.k]
	for {
		t := &e.ws.slots[e.ci%e.ring]
		t.gen = e.gen(e.ci)
		live := t.active[:0]
		for _, pc := range t.active {
			if t.stamp[pc] == t.gen {
				live = append(live, pc)
			}
		}
		t.active = live

		if len(t.active) == 0 && !e.matched && e.prog.scan.enabled &&
			e.pos < len(e.subject) {
			if !e.bolAt(prev) {
				// No thread is live, and no match can begin on a byte outside the filter.
				// The scan therefore jumps to the next stop byte.
				next := e.scanAhead()
				if next > e.pos {
					e.pos = next
					e.ci++
					prev = 'x'
					if e.pos > 0 && e.subject[e.pos-1] == '\n' {
						prev = '\n'
					}
					continue
				}
			}
		}

		atEnd := e.pos == len(e.subject)
		e.cur, e.size = -2, 0
		if !atEnd {
			e.cur, e.size = decodeRune(e.subject[e.pos:])
		}
		e.bol = e.bolAt(prev)
		e.eol = (atEnd && e.eflags&NotEOL == 0) ||
			(e.nlMode && e.cur == '\n')

		e.ws.queue = append(e.ws.queue[:0], t.active...)
		if !e.matched {
			e.relax(t, e.prog.start, int32(e.pos), zeros)
		}
		e.closure(t)

		if atEnd {
			return
		}
		e.consume(t)

		// Stop when nothing is in flight and spawning has stopped.
		if e.matched {
			pendingWork := false
			for delta := 1; delta < e.ring; delta++ {
				future := &e.ws.slots[(e.ci+delta)%e.ring]
				if future.gen == e.gen(e.ci+delta) && len(future.active) > 0 {
					pendingWork = true
					break
				}
			}
			if !pendingWork {
				return
			}
		}

		prev = e.cur
		e.pos += e.size
		e.ci++
	}
}
