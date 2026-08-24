package revera

// This file lowers the AST into a flat instruction program for the
// phase A executor. Phase A only needs the match relation, the
// minimal repetition counters, and anchors; groups compile to
// nothing here.

// Instruction opcodes.
const (
	iRune     uint8 = 0 // consume one character equal to arg
	iRuneFold uint8 = 1 // consume one character in foldSets[arg]
	iAny      uint8 = 2 // consume any character dot accepts
	iBracket  uint8 = 3 // consume per the bracket arena entry arg
	iBOL      uint8 = 4 // assert a line beginning
	iEOL      uint8 = 5 // assert a line end
	iSplit    uint8 = 6 // continue at next and at alt
	iJmp      uint8 = 7 // continue at next
	iMatch    uint8 = 8 // accept
	iFail     uint8 = 9 // dead end for a pruned oversized subtree
)

type instr struct {
	op   uint8
	next uint32
	alt  uint32
	arg  uint32
	// mask marks the shortest-preferring repetitions this consuming
	// instruction sits inside, one bit per counter slot below 64.
	mask uint64
	// extra lists counter slots at 64 and above; almost always nil.
	extra []uint32
}

// scanFilter speeds up positions where no thread is live. It holds
// the bytes that can begin a match from an ordinary mid-subject
// boundary. The executor may skip every byte outside the set: no
// match can start there, because the start closure at such a
// boundary reaches only the listed consuming instructions and no
// accepting state.
type scanFilter struct {
	enabled bool
	single  bool
	b       uint8
	stop    [256]bool
}

type program struct {
	ins      []instr
	start    uint32
	foldSets [][]int32
	// multi is true when some bracket can consume several characters.
	multi bool
	// failMin is the smallest minimum match length over every
	// subtree that was pruned to iFail for size. The program is
	// exact for any subject with fewer characters, because a pruned
	// subtree cannot participate in a match of such a subject.
	// failMinNone means nothing was pruned; a pruned subtree's
	// saturated minimum can legitimately reach lenInf, so lenInf
	// cannot be the sentinel.
	failMin int
	scan    scanFilter
}

// failMinNone marks a program with no pruned subtree.
const failMinNone = int(1) << 62

// maxProgram caps the expanded program size. Interval expansion can
// multiply nested counts. Compilation still succeeds past the cap;
// the expression then answers through the minimum-length fallback,
// and only a subject long enough to need the huge program reports
// ESpace.
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
	prog    program
	tooBig  bool
	failMin int
	errCode int32
	icase   bool
}

// buildScanFilter derives the skip byte set for mid-subject
// boundaries. It walks epsilon transitions from the start with both
// anchors off, records the first bytes of the reachable consuming
// instructions, and gives up when dot, a bracket, or a reachable
// accept makes the filter unsound. newlineMode forces a stop at
// every newline, because anchors gain line boundaries there.
func buildScanFilter(pr *program, newlineMode bool) {
	ok := true
	matchReachable := false
	seen := make([]bool, len(pr.ins))
	stack := make([]uint32, 0, 16)
	stack = append(stack, pr.start)
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pc] {
			continue
		}
		seen[pc] = true
		switch pr.ins[pc].op {
		case iSplit:
			stack = append(stack, pr.ins[pc].next, pr.ins[pc].alt)
		case iJmp:
			stack = append(stack, pr.ins[pc].next)
		case iBOL:
			// Anchors are off at a mid-subject boundary.
		case iEOL:
			// Same as iBOL.
		case iMatch:
			matchReachable = true
		case iFail:
			// A pruned dead end reaches nothing.
		case iRune:
			pr.scan.stop[utf8LeadByte(int32(pr.ins[pc].arg))] = true
		case iRuneFold:
			set := pr.ins[pc].arg
			for i := 0; i < len(pr.foldSets[set]); i++ {
				pr.scan.stop[utf8LeadByte(pr.foldSets[set][i])] = true
			}
		default:
			// Dot and brackets accept too much to filter.
			ok = false
		}
	}
	if matchReachable || !ok || pr.multi {
		var none scanFilter
		pr.scan = none
		return
	}
	if newlineMode {
		pr.scan.stop['\n'] = true
	}
	pr.scan.enabled = true
	count := 0
	for b := 0; b < 256; b++ {
		if pr.scan.stop[b] {
			count++
			pr.scan.b = uint8(b)
		}
	}
	pr.scan.single = count == 1
}

// instrEstimate bounds the instruction count one node expands into.
func instrEstimate(nodes []node, ni int32) int {
	estimateCap := int(maxProgram) * 4
	total := 0
	switch nodes[ni].op {
	case opGroup:
		return instrEstimate(nodes, nodes[ni].ch[0])
	case opConcat:
		for i := 0; i < len(nodes[ni].ch); i++ {
			total += instrEstimate(nodes, nodes[ni].ch[i])
			if total > estimateCap {
				return estimateCap
			}
		}
		return total
	case opAlt:
		total = len(nodes[ni].ch)
		for i := 0; i < len(nodes[ni].ch); i++ {
			total += instrEstimate(nodes, nodes[ni].ch[i])
			if total > estimateCap {
				return estimateCap
			}
		}
		return total
	case opRepeat:
		copies := nodes[ni].max
		if nodes[ni].max == infinite {
			copies = nodes[ni].min + 1
		}
		if copies == 0 {
			return 1
		}
		child := instrEstimate(nodes, nodes[ni].ch[0])
		total = copies*child + copies + 2
		if total > estimateCap || total < 0 {
			return estimateCap
		}
		return total
	}
	return 1
}

// compileProgram lowers the AST into b.prog. The caller sets icase
// before the call and reads tooBig and errCode after it: tooBig with
// a zero code means the expansion passed the size cap, and execution
// then uses the minimum-length fallback.
func compileProgram(b *progBuilder, nodes []node, root int32, multi bool, newlineMode bool) {
	b.failMin = failMinNone
	body := emit(b, nodes, root, 0, nil)
	if b.errCode != ErrNone {
		return
	}
	var m instr
	m.op = iMatch
	match := addInstr(b, m)
	if b.errCode != ErrNone || b.tooBig {
		return
	}
	patch(b, body.out, match)
	b.prog.start = body.start
	b.prog.multi = multi
	b.prog.failMin = b.failMin
	buildScanFilter(&b.prog, newlineMode)
}

func addInstr(b *progBuilder, ins instr) uint32 {
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

func patch(b *progBuilder, slots []patchSlot, target uint32) {
	if b.tooBig {
		return
	}
	for i := 0; i < len(slots); i++ {
		if slots[i].alt {
			b.prog.ins[slots[i].idx].alt = target
		} else {
			b.prog.ins[slots[i].idx].next = target
		}
	}
}

// singleOut builds the out list of a one-exit fragment.
func singleOut(idx uint32, alt bool) []patchSlot {
	var slot patchSlot
	slot.idx = idx
	slot.alt = alt
	out := make([]patchSlot, 0, 1)
	return append(out, slot)
}

// epsilonFrag returns a fragment that consumes nothing.
func epsilonFrag(b *progBuilder) frag {
	var j instr
	j.op = iJmp
	idx := addInstr(b, j)
	var f frag
	f.start = idx
	f.out = singleOut(idx, false)
	return f
}

// copyExtra duplicates an extra-slot list, so instructions never
// share one buffer.
func copyExtra(extra []uint32) []uint32 {
	if len(extra) == 0 {
		return nil
	}
	dup := make([]uint32, len(extra))
	copy(dup, extra)
	return dup
}

// consumeFrag appends one consuming instruction and wraps it as a
// fragment.
func consumeFrag(b *progBuilder, op uint8, arg uint32, mask uint64, extra []uint32) frag {
	var ins instr
	ins.op = op
	ins.arg = arg
	ins.mask = mask
	ins.extra = copyExtra(extra)
	idx := addInstr(b, ins)
	var f frag
	f.start = idx
	f.out = singleOut(idx, false)
	return f
}

func emit(b *progBuilder, nodes []node, ni int32, mask uint64, extra []uint32) frag {
	var none frag
	if b.errCode != ErrNone || b.tooBig {
		return none
	}
	switch nodes[ni].op {
	case opChar:
		if b.icase {
			arg := uint32(len(b.prog.foldSets))
			foldCopy := make([]int32, len(nodes[ni].fold))
			copy(foldCopy, nodes[ni].fold)
			b.prog.foldSets = append(b.prog.foldSets, foldCopy)
			return consumeFrag(b, iRuneFold, arg, mask, extra)
		}
		return consumeFrag(b, iRune, uint32(nodes[ni].r), mask, extra)
	case opAny:
		return consumeFrag(b, iAny, 0, mask, extra)
	case opBracket:
		return consumeFrag(b, iBracket, uint32(nodes[ni].br), mask, extra)
	case opBOL:
		return consumeFrag(b, iBOL, 0, 0, nil)
	case opEOL:
		return consumeFrag(b, iEOL, 0, 0, nil)
	case opGroup:
		return emit(b, nodes, nodes[ni].ch[0], mask, extra)
	case opConcat:
		result := emit(b, nodes, nodes[ni].ch[0], mask, extra)
		for i := 1; i < len(nodes[ni].ch); i++ {
			next := emit(b, nodes, nodes[ni].ch[i], mask, extra)
			if b.errCode != ErrNone {
				return none
			}
			patch(b, result.out, next.start)
			result.out = next.out
		}
		return result
	case opAlt:
		return emitAlt(b, nodes, ni, mask, extra)
	case opRepeat:
		return emitRepeat(b, nodes, ni, mask, extra)
	}
	b.errCode = ErrBadPat
	return none
}

func emitAlt(b *progBuilder, nodes []node, ni int32, mask uint64, extra []uint32) frag {
	var none frag
	var result frag
	splits := make([]uint32, 0, len(nodes[ni].ch))
	count := len(nodes[ni].ch)
	for i := 0; i < count; i++ {
		if i < count-1 {
			var s instr
			s.op = iSplit
			splits = append(splits, addInstr(b, s))
		}
		sub := emit(b, nodes, nodes[ni].ch[i], mask, extra)
		if b.errCode != ErrNone || b.tooBig {
			return none
		}
		if i < count-1 {
			b.prog.ins[splits[i]].next = sub.start
		} else {
			b.prog.ins[splits[i-1]].alt = sub.start
		}
		if i > 0 && i < count-1 {
			b.prog.ins[splits[i-1]].alt = splits[i]
		}
		result.out = append(result.out, sub.out...)
	}
	result.start = splits[0]
	return result
}

// fragAppend chains f after result, or starts result with f. It
// returns true so callers can update their have flag in one line.
func fragAppend(b *progBuilder, result *frag, have bool, f frag) bool {
	if !have {
		result.start = f.start
		result.out = f.out
		return true
	}
	patch(b, result.out, f.start)
	result.out = f.out
	return true
}

func emitRepeat(b *progBuilder, nodes []node, ni int32, mask uint64, extra []uint32) frag {
	var none frag
	if instrEstimate(nodes, ni) > maxProgram-len(b.prog.ins) {
		// Prune the oversized expansion to a dead end. The program
		// stays exact for subjects shorter than this subtree's
		// minimum match length; Exec checks that bound.
		var fi instr
		fi.op = iFail
		idx := addInstr(b, fi)
		b.failMin = min(b.failMin, nodes[ni].minL)
		var f frag
		f.start = idx
		return f
	}
	if nodes[ni].minimal {
		slot := nodes[ni].index
		if slot < maskWidth {
			mask |= uint64(1) << slot
		} else {
			// The copy keeps sibling fragments from sharing the
			// grown list.
			grown := make([]uint32, len(extra)+1)
			copy(grown, extra)
			grown[len(extra)] = uint32(slot)
			extra = grown
		}
	}
	child := nodes[ni].ch[0]
	lo := nodes[ni].min
	hi := nodes[ni].max

	if lo == 0 && hi == 0 {
		return epsilonFrag(b)
	}

	var result frag
	haveResult := false

	// Required copies: the last one loops when the maximum is
	// unbounded.
	for i := 0; i < lo; i++ {
		if b.errCode != ErrNone || b.tooBig {
			return none
		}
		if i == lo-1 && hi == infinite {
			haveResult = fragAppend(b, &result, haveResult,
				emitPlus(b, nodes, child, mask, extra))
			return result
		}
		haveResult = fragAppend(b, &result, haveResult,
			emit(b, nodes, child, mask, extra))
	}

	if hi == infinite {
		// The minimum is zero here: a plain star.
		haveResult = fragAppend(b, &result, haveResult,
			emitStar(b, nodes, child, mask, extra))
		return result
	}

	// Optional copies: each may skip straight to the shared exit.
	skips := make([]patchSlot, 0, max(hi-lo, 1))
	for i := lo; i < hi; i++ {
		if b.errCode != ErrNone || b.tooBig {
			return none
		}
		var s instr
		s.op = iSplit
		split := addInstr(b, s)
		sub := emit(b, nodes, child, mask, extra)
		if b.errCode != ErrNone || b.tooBig {
			return none
		}
		b.prog.ins[split].next = sub.start
		var slot patchSlot
		slot.idx = split
		slot.alt = true
		skips = append(skips, slot)
		var piece frag
		piece.start = split
		piece.out = sub.out
		haveResult = fragAppend(b, &result, haveResult, piece)
	}
	result.out = append(result.out, skips...)
	if !haveResult {
		return epsilonFrag(b)
	}
	return result
}

func emitStar(b *progBuilder, nodes []node, child int32, mask uint64, extra []uint32) frag {
	var none frag
	var s instr
	s.op = iSplit
	split := addInstr(b, s)
	body := emit(b, nodes, child, mask, extra)
	if b.errCode != ErrNone || b.tooBig {
		return none
	}
	b.prog.ins[split].next = body.start
	patch(b, body.out, split)
	var f frag
	f.start = split
	f.out = singleOut(split, true)
	return f
}

func emitPlus(b *progBuilder, nodes []node, child int32, mask uint64, extra []uint32) frag {
	var none frag
	body := emit(b, nodes, child, mask, extra)
	if b.errCode != ErrNone || b.tooBig {
		return none
	}
	var s instr
	s.op = iSplit
	split := addInstr(b, s)
	b.prog.ins[split].next = body.start
	patch(b, body.out, split)
	var f frag
	f.start = body.start
	f.out = singleOut(split, true)
	return f
}
