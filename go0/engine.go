package revera

// This file holds the phase A executor. It scans the subject once, runs
// every viable automaton path in lockstep, and returns the selected match
// start and end. Thread payloads hold the match start and the minimal
// repetition counters; captures are phase B's job.
//
// Merging keeps the best (start, counters) payload per instruction and
// position. Futures from a shared instruction are identical and counters
// only grow, so the merge preserves the selected candidate.

import (
	"slices"
	"strings"
)

// maxElemAhead is the largest character count one transition can consume.
// It matches the locale data's longest collating element.
const maxElemAhead = 8

// slotTable holds the payloads for one character boundary.
type slotTable struct {
	stamp  []uint32
	starts []int32
	ctr    []uint32
	active []uint32
	gen    uint32
	count  int
}

type engineWS struct {
	slots   []slotTable
	queue   []uint32
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
		t.count = 0
	}
	if len(ws.bestCtr) < k {
		ws.bestCtr = make([]uint32, k)
		ws.ctrBuf = make([]uint32, k)
		ws.zeros = make([]uint32, k)
	}
	ws.queue = ws.queue[:0]
}

// ctrLess compares two counter vectors lexicographically.
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

// phaseA carries the per-call executor state, so the hot path builds no
// closures and allocates nothing after workspace warmup.
type phaseA struct {
	re      *Regexp
	prog    *program
	ws      *engineWS
	subject string
	eflags  ExecFlags
	k       int
	ring    int
	icase   bool
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

func (re *Regexp) runPhaseA(subject string, eflags ExecFlags) (engineResult, *Error) {
	ws := re.getWS()
	e := phaseA{
		re:      re,
		prog:    re.prog,
		ws:      ws,
		subject: subject,
		eflags:  eflags,
		k:       re.minSlots,
		ring:    2,
		icase:   re.flags&ICase != 0,
		nlMode:  re.flags&Newline != 0,
	}
	if e.prog.multi {
		e.ring = maxElemAhead + 1
	}
	ws.prepare(e.prog, e.k, e.ring)
	e.bestCtr = ws.bestCtr[:e.k]
	e.run()
	ws.base = e.gen(e.ci) + uint32(e.ring) + 1
	re.putWS(ws)
	return engineResult{matched: e.matched, so: e.so, eo: e.eo}, nil
}

func (e *phaseA) gen(boundary int) uint32 {
	return e.ws.base + uint32(boundary) + 1
}

// consider records a match candidate: earliest start, then smallest
// counters, then longest end.
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

// store merges a payload into a slot and reports whether it was kept.
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
		t.count++
	}
	t.starts[pc] = start
	if e.k > 0 {
		copy(t.ctr[int(pc)*e.k:int(pc)*e.k+e.k], ctr)
	}
	return true
}

// relax merges a payload at the current boundary and queues it for the
// epsilon closure.
func (e *phaseA) relax(t *slotTable, pc uint32, start int32, ctr []uint32) {
	if e.store(t, pc, start, ctr) {
		e.ws.queue = append(e.ws.queue, pc)
	}
}

// closure drains the relaxation queue over the epsilon instructions.
func (e *phaseA) closure(t *slotTable) {
	for len(e.ws.queue) > 0 {
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
// delta counts consumed characters; they also increment the counters
// selected by the instruction's mask.
func (e *phaseA) arrive(pc uint32, delta int, start int32, ctr []uint32) {
	future := &e.ws.slots[(e.ci+delta)%e.ring]
	g := e.gen(e.ci + delta)
	if future.gen != g {
		future.gen = g
		future.active = future.active[:0]
		future.count = 0
	}
	newCtr := ctr
	if e.k > 0 {
		ins := &e.prog.ins[pc]
		buffer := e.ws.ctrBuf[:e.k]
		copy(buffer, ctr)
		if ins.mask != 0 {
			limit := min(e.k, maskWidth)
			for slot := range limit {
				if ins.mask&(1<<uint(slot)) != 0 {
					buffer[slot] += uint32(delta)
				}
			}
		}
		for _, slot := range ins.extra {
			buffer[slot] += uint32(delta)
		}
		newCtr = buffer
	}
	e.store(future, e.prog.ins[pc].next, start, newCtr)
}

// consume advances every consuming instruction over the current
// character or collating element.
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
			if br.matchesOne(e.cur, e.re.loc, e.icase, e.nlMode) {
				e.arrive(pc, 1, start, ctr)
			}
			if !br.hasMultiMembers() {
				continue
			}
			if !aheadReady {
				e.decodeAhead()
				aheadReady = true
			}
			for length := 2; length <= len(e.ws.ahead); length++ {
				if br.matchesMulti(e.ws.ahead[:length], e.re.loc, e.icase) {
					e.arrive(pc, length, start, ctr)
				}
			}
		}
	}
}

// decodeAhead fills the lookahead buffer with up to maxElemAhead
// characters starting at the current position.
func (e *phaseA) decodeAhead() {
	e.ws.ahead = e.ws.ahead[:0]
	at := e.pos
	for len(e.ws.ahead) < maxElemAhead && at < len(e.subject) {
		r, size := decodeRune(e.subject[at:])
		e.ws.ahead = append(e.ws.ahead, r)
		at += size
	}
}

// scanAhead returns the offset of the next byte in the stop set, or the
// subject length. Stop bytes are ASCII or UTF-8 lead bytes, so the
// returned offset is always a boundary the sequential scan would visit.
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

func (e *phaseA) run() {
	var prev rune = -2
	zeros := e.ws.zeros[:e.k]
	for {
		t := &e.ws.slots[e.ci%e.ring]
		t.gen = e.gen(e.ci)
		t.count = 0
		live := t.active[:0]
		for _, pc := range t.active {
			if t.stamp[pc] == t.gen {
				live = append(live, pc)
				t.count++
			}
		}
		t.active = live

		if t.count == 0 && !e.matched && e.prog.scan.enabled &&
			e.pos < len(e.subject) {
			bolHere := (e.pos == 0 && e.eflags&NotBOL == 0) ||
				(e.nlMode && prev == '\n')
			if !bolHere {
				// No thread is live and no match can begin on a byte
				// outside the filter, so jump to the next stop byte.
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
		e.bol = (e.pos == 0 && e.eflags&NotBOL == 0) ||
			(e.nlMode && prev == '\n')
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
				if future.gen == e.gen(e.ci+delta) && future.count > 0 {
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
