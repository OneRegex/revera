package reference

// This file lowers the AST into a flat instruction program for the phase A executor.
// Phase A only needs the match relation, the minimal repetition counters, and anchors.
// Groups compile to nothing here.

import (
	"math"
	"slices"
	"unicode/utf8"
)

type iop uint8

const (
	iRune     iop = iota // consume one character equal to arg
	iRuneFold            // consume one character in foldSets[arg]
	iAny                 // consume any character dot accepts
	iBracket             // consume per brackets[arg]
	iBOL                 // assert a line beginning
	iEOL                 // assert a line end
	iSplit               // continue at next and at alt
	iJmp                 // continue at next
	iMatch               // accept
	iFail                // dead end for a pruned oversized subtree
)

type instr struct {
	op   iop
	next uint32
	alt  uint32
	arg  uint32
	// mask marks the shortest-preferring repetitions this consuming instruction sits inside.
	// It holds one bit per counter slot below 64.
	mask uint64
	// extra lists counter slots at 64 and above, and is almost always nil.
	extra []uint32
}

type program struct {
	ins      []instr
	start    uint32
	foldSets [][]rune
	brackets []*bracketSet
	// multi is true when some bracket can consume several characters.
	multi bool
	// failMin is the smallest minimum match length over every subtree that size pruning replaced with iFail.
	// The program stays exact for any subject with fewer characters.
	// A pruned subtree cannot take part in such a match.
	// failMinNone means that nothing was pruned.
	// The saturated minimum of a pruned subtree can legitimately reach lenInf, so lenInf cannot be the sentinel.
	failMin int
	scan    scanFilter
}

// scanFilter speeds up positions where no thread is live.
// It holds the bytes that can begin a match from an ordinary mid-subject boundary.
// The executor may skip every byte outside the set, because no match can start there.
// The start closure at such a boundary reaches only the listed consuming instructions, and no accepting state.
type scanFilter struct {
	enabled bool
	single  bool
	b       byte
	stop    [256]bool
}

// closureScan walks epsilon transitions from the start, in the given anchor context.
// It reports the reachable consuming instructions.
// It also reports whether an accepting state is reachable without any consumption.
func (p *program) closureScan(bol, eol bool, visit func(pc uint32)) (matchReachable bool) {
	seen := make([]bool, len(p.ins))
	stack := []uint32{p.start}
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pc] {
			continue
		}
		seen[pc] = true
		ins := &p.ins[pc]
		switch ins.op {
		case iSplit:
			stack = append(stack, ins.next, ins.alt)
		case iJmp:
			stack = append(stack, ins.next)
		case iBOL:
			if bol {
				stack = append(stack, ins.next)
			}
		case iEOL:
			if eol {
				stack = append(stack, ins.next)
			}
		case iMatch:
			matchReachable = true
		case iFail:
			// A pruned dead end reaches nothing.
		default:
			visit(pc)
		}
	}
	return matchReachable
}

// buildScanFilter derives the skip byte set for mid-subject boundaries.
// newlineMode forces a stop at every newline, because anchors gain line boundaries there.
func (p *program) buildScanFilter(newlineMode bool) {
	ok := true
	addRune := func(r rune) {
		var buf [4]byte
		p.scan.stop[utf8.AppendRune(buf[:0], r)[0]] = true
	}
	matchReachable := p.closureScan(false, false, func(pc uint32) {
		ins := &p.ins[pc]
		switch ins.op {
		case iRune:
			addRune(rune(ins.arg))
		case iRuneFold:
			for _, f := range p.foldSets[ins.arg] {
				addRune(f)
			}
		default:
			// Dot and brackets accept too much to filter.
			ok = false
		}
	})
	if matchReachable || !ok || p.multi {
		p.scan = scanFilter{}
		return
	}
	if newlineMode {
		p.scan.stop['\n'] = true
	}
	p.scan.enabled = true
	count := 0
	for b := range 256 {
		if p.scan.stop[b] {
			count++
			p.scan.b = byte(b)
		}
	}
	p.scan.single = count == 1
}

// failMinNone marks a program with no pruned subtree.
const failMinNone = math.MaxInt

// maxProgram caps the expanded program size, because interval expansion can multiply nested counts.
// Compilation still succeeds past the cap.
// The expression then answers through the minimum-length fallback.
// Only a subject long enough to need the huge program reports ESpace.
const maxProgram = 1 << 20

// maskWidth is the number of counter slots one mask word covers.
const maskWidth = 64

type patchSlot struct {
	idx uint32
	alt bool
}

type frag struct {
	start uint32
	out   []patchSlot
}

type progBuilder struct {
	re      *Regexp
	prog    *program
	tooBig  bool
	failMin int
	err     *Error
}

// instrEstimate bounds the instruction count one node expands into.
func instrEstimate(n *node) int64 {
	const estimateCap = int64(maxProgram) * 4
	var total int64
	switch n.op {
	case opGroup:
		return instrEstimate(n.ch[0])
	case opConcat:
		for _, child := range n.ch {
			total += instrEstimate(child)
			if total > estimateCap {
				return estimateCap
			}
		}
		return total
	case opAlt:
		total = int64(len(n.ch))
		for _, child := range n.ch {
			total += instrEstimate(child)
			if total > estimateCap {
				return estimateCap
			}
		}
		return total
	case opRepeat:
		copies := int64(n.max)
		if n.max == infinite {
			copies = int64(n.min) + 1
		}
		if copies == 0 {
			return 1
		}
		child := instrEstimate(n.ch[0])
		total = copies*child + copies + 2
		if total > estimateCap || total < 0 {
			return estimateCap
		}
		return total
	}
	return 1
}

// compileProgram lowers the AST.
// A nil program with a nil error means the expansion passed the size cap.
// Execution then uses the minimum-length fallback.
func compileProgram(re *Regexp) (*program, *Error) {
	b := &progBuilder{re: re, prog: &program{}, failMin: failMinNone}
	body := b.emit(re.root, 0, nil)
	if b.err != nil {
		return nil, b.err
	}
	match := b.add(instr{op: iMatch})
	if b.err != nil {
		return nil, b.err
	}
	if b.tooBig {
		return nil, nil
	}
	b.patch(body.out, match)
	b.prog.start = body.start
	b.prog.multi = re.multi
	b.prog.failMin = b.failMin
	b.prog.buildScanFilter(re.flags&Newline != 0)
	return b.prog, nil
}

func (b *progBuilder) add(ins instr) uint32 {
	if b.tooBig {
		return 0
	}
	if len(b.prog.ins) >= maxProgram {
		b.tooBig = true
		return 0
	}
	b.prog.ins = append(b.prog.ins, ins)
	return uint32(len(b.prog.ins) - 1)
}

func (b *progBuilder) patch(slots []patchSlot, target uint32) {
	if b.tooBig {
		return
	}
	for _, slot := range slots {
		if slot.alt {
			b.prog.ins[slot.idx].alt = target
		} else {
			b.prog.ins[slot.idx].next = target
		}
	}
}

// epsilon returns a fragment that consumes nothing.
func (b *progBuilder) epsilon() frag {
	idx := b.add(instr{op: iJmp})
	return frag{start: idx, out: []patchSlot{{idx, false}}}
}

func (b *progBuilder) emit(n *node, mask uint64, extra []uint32) frag {
	if b.err != nil || b.tooBig {
		return frag{}
	}
	switch n.op {
	case opChar:
		if b.re.flags&ICase != 0 {
			arg := uint32(len(b.prog.foldSets))
			b.prog.foldSets = append(b.prog.foldSets, n.fold)
			idx := b.add(instr{op: iRuneFold, arg: arg, mask: mask, extra: extra})
			return frag{start: idx, out: []patchSlot{{idx, false}}}
		}
		idx := b.add(instr{op: iRune, arg: uint32(n.r), mask: mask, extra: extra})
		return frag{start: idx, out: []patchSlot{{idx, false}}}
	case opAny:
		idx := b.add(instr{op: iAny, mask: mask, extra: extra})
		return frag{start: idx, out: []patchSlot{{idx, false}}}
	case opBracket:
		arg := uint32(len(b.prog.brackets))
		b.prog.brackets = append(b.prog.brackets, n.br)
		idx := b.add(instr{op: iBracket, arg: arg, mask: mask, extra: extra})
		return frag{start: idx, out: []patchSlot{{idx, false}}}
	case opBOL:
		idx := b.add(instr{op: iBOL})
		return frag{start: idx, out: []patchSlot{{idx, false}}}
	case opEOL:
		idx := b.add(instr{op: iEOL})
		return frag{start: idx, out: []patchSlot{{idx, false}}}
	case opGroup:
		return b.emit(n.ch[0], mask, extra)
	case opConcat:
		result := b.emit(n.ch[0], mask, extra)
		for _, child := range n.ch[1:] {
			next := b.emit(child, mask, extra)
			if b.err != nil {
				return frag{}
			}
			b.patch(result.out, next.start)
			result.out = next.out
		}
		return result
	case opAlt:
		return b.emitAlt(n, mask, extra)
	case opRepeat:
		return b.emitRepeat(n, mask, extra)
	}
	b.err = compileError(BadPat, -1)
	return frag{}
}

func (b *progBuilder) emitAlt(n *node, mask uint64, extra []uint32) frag {
	var out []patchSlot
	var splits []uint32
	for i, branch := range n.ch {
		if i < len(n.ch)-1 {
			splits = append(splits, b.add(instr{op: iSplit}))
		}
		sub := b.emit(branch, mask, extra)
		if b.err != nil || b.tooBig {
			return frag{}
		}
		if i < len(n.ch)-1 {
			b.prog.ins[splits[i]].next = sub.start
		} else {
			b.prog.ins[splits[i-1]].alt = sub.start
		}
		if i > 0 && i < len(n.ch)-1 {
			b.prog.ins[splits[i-1]].alt = splits[i]
		}
		out = append(out, sub.out...)
	}
	return frag{start: splits[0], out: out}
}

func (b *progBuilder) emitRepeat(n *node, mask uint64, extra []uint32) frag {
	if instrEstimate(n) > int64(maxProgram-len(b.prog.ins)) {
		// The oversized expansion becomes a dead end.
		// The program stays exact for subjects shorter than the minimum match length of this subtree, and Exec checks that bound.
		idx := b.add(instr{op: iFail})
		b.failMin = min(b.failMin, n.minL)
		return frag{start: idx}
	}
	if n.minimal {
		if n.index < maskWidth {
			mask |= 1 << uint(n.index)
		} else {
			// Clip forces a copy, so sibling fragments never share the grown list.
			extra = append(slices.Clip(extra), uint32(n.index))
		}
	}
	child := n.ch[0]
	min, max := n.min, n.max

	if min == 0 && max == 0 {
		return b.epsilon()
	}

	var result frag
	haveResult := false
	appendFrag := func(f frag) {
		if !haveResult {
			result = f
			haveResult = true
			return
		}
		b.patch(result.out, f.start)
		result.out = f.out
	}

	// These are the required copies.
	// The last one loops when max is unbounded.
	for i := range min {
		if b.err != nil || b.tooBig {
			return frag{}
		}
		if i == min-1 && max == infinite {
			appendFrag(b.emitPlus(child, mask, extra))
			return result
		}
		appendFrag(b.emit(child, mask, extra))
	}

	if max == infinite {
		// min is zero here, so this is a plain star.
		appendFrag(b.emitStar(child, mask, extra))
		return result
	}

	// These are the optional copies, and each one may skip straight to the shared exit.
	var skips []patchSlot
	for i := min; i < max; i++ {
		if b.err != nil || b.tooBig {
			return frag{}
		}
		split := b.add(instr{op: iSplit})
		sub := b.emit(child, mask, extra)
		if b.err != nil || b.tooBig {
			return frag{}
		}
		b.prog.ins[split].next = sub.start
		skips = append(skips, patchSlot{split, true})
		appendFrag(frag{start: split, out: sub.out})
	}
	result.out = append(result.out, skips...)
	if !haveResult {
		return b.epsilon()
	}
	return result
}

func (b *progBuilder) emitStar(child *node, mask uint64, extra []uint32) frag {
	split := b.add(instr{op: iSplit})
	body := b.emit(child, mask, extra)
	if b.err != nil || b.tooBig {
		return frag{}
	}
	b.prog.ins[split].next = body.start
	b.patch(body.out, split)
	return frag{start: split, out: []patchSlot{{split, true}}}
}

func (b *progBuilder) emitPlus(child *node, mask uint64, extra []uint32) frag {
	body := b.emit(child, mask, extra)
	if b.err != nil || b.tooBig {
		return frag{}
	}
	split := b.add(instr{op: iSplit})
	b.prog.ins[split].next = body.start
	b.patch(body.out, split)
	return frag{start: body.start, out: []patchSlot{{split, true}}}
}
