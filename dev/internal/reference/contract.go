package reference

// This file computes resource contracts.
// A contract bounds, for each backend, what one Exec call can use on a subject of a given maximum length.
// An application can compare the figures against its budget and refuse a pattern before it ever runs.
//
// Heap figures count the explicit allocations the code performs, with fixed 64-bit field sizes.
// They are therefore the same on every platform.
// They leave out runtime object headers, map buckets, allocator rounding, and garbage collection.
// The true footprint stays a bounded constant factor above the figure.
// Stack figures multiply the deepest possible call chain by a fixed per-frame estimate.
// Step figures count abstract unit-cost operations, not nanoseconds.
// They are worst-case bounds, and ordinary subjects stay far below them.

import "github.com/oneregex/revera/dev/internal/reference/locale"

// BackendContract bounds the resources one backend can use during a single Exec call.
type BackendContract struct {
	// HeapBytes bounds the explicit heap allocations, in bytes.
	HeapBytes int64
	// StackBytes estimates the deepest call stack, in bytes.
	StackBytes int64
	// Steps bounds the abstract operations the backend can perform.
	Steps int64
}

// Contract bounds the resources of one Exec call on a subject of at most MaxInput bytes.
// MaxInput is the bound the figures cover.
// It is the requested value, clamped to the subject limit of Exec and to zero from below.
// Execution has three backends:
//
//   - Matcher is the phase A automaton.
//     It always runs, and it answers every call that needs no group offsets.
//   - OnePass is the phase B capture walk.
//     It is set when compilation proved that every span has one parse.
//     Its figures apply when the walk succeeds, which the proof guarantees.
//   - Solver is the phase B memoized parse search.
//     It is the guaranteed ceiling for any call that fills group offsets.
//     The walk falls back to it on an inconsistency.
//     It is nil when the pattern was compiled with NoSub, and when Exec cannot reach phase B.
//
// Every value saturates at 1<<62, which marks a bound too large to be useful.
// The pooled per-Regexp workspace counts once.
// Concurrent Exec calls on the same Regexp each use their own copy of it.
// Both capture backends include the shared window allocations, so nothing may sum their heap fields.
// The HeapBytes method takes the maximum instead.
type Contract struct {
	MaxInput int
	Matcher  BackendContract
	OnePass  *BackendContract
	Solver   *BackendContract
}

// contractCap saturates contract arithmetic.
// It leaves room to add saturated values without overflow.
const contractCap = int64(1) << 62

// Fixed 64-bit sizes of the records the contract counts, in bytes.
const (
	// frameBytes estimates one recursive call frame.
	frameBytes = 256
	// matcherStackBytes covers the fixed frames of the iterative phase A executor.
	matcherStackBytes = 2048
	// equivFrames bounds the equivCandidate recursion.
	// It allows one frame per character of a multi-character collating element, plus slack.
	equivFrames = maxElemAhead + 2
	// errorBytes is one allocated Error value.
	errorBytes = 32
	// ptreeBytes is the size of one solver parse-tree node.
	ptreeBytes = 56
	// mapEntryBytes is the largest memo record: the bestRep key and its result.
	mapEntryBytes = 72
	// matchBytes is the size of one Match.
	matchBytes = 16
)

func cAdd(a, b int64) int64 {
	if a > contractCap-b {
		return contractCap
	}
	return a + b
}

func cMul(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > contractCap/b {
		return contractCap
	}
	return a * b
}

// CompileWithContract compiles the pattern like Compile.
// It also returns the resource contract for subjects of at most maxInput bytes.
// A larger maxInput clamps to the subject limit of Exec.
func CompileWithContract(pattern string, loc locale.Locale, flags CompileFlags, maxInput int) (*Regexp, *Contract, error) {
	re, err := Compile(pattern, loc, flags)
	if err != nil {
		return nil, nil, err
	}
	return re, re.newContract(maxInput), nil
}

// HeapBytes bounds the heap of one whole call.
// It adds the matcher workspace and the most expensive capture backend that can run.
func (c *Contract) HeapBytes() int64 {
	capture := int64(0)
	if c.OnePass != nil {
		capture = c.OnePass.HeapBytes
	}
	if c.Solver != nil {
		capture = max(capture, c.Solver.HeapBytes)
	}
	return cAdd(c.Matcher.HeapBytes, capture)
}

// StackBytes bounds the stack of one whole call.
func (c *Contract) StackBytes() int64 {
	deepest := c.Matcher.StackBytes
	if c.OnePass != nil {
		deepest = max(deepest, c.OnePass.StackBytes)
	}
	if c.Solver != nil {
		deepest = max(deepest, c.Solver.StackBytes)
	}
	return deepest
}

// Steps bounds the operations of one whole call.
// The walk can fall back to the solver, so both capture backends count.
func (c *Contract) Steps() int64 {
	steps := c.Matcher.Steps
	if c.OnePass != nil {
		steps = cAdd(steps, c.OnePass.Steps)
	}
	if c.Solver != nil {
		steps = cAdd(steps, c.Solver.Steps)
	}
	return steps
}

func (re *Regexp) newContract(maxInput int) *Contract {
	length := min(int64(max(maxInput, 0)), subjectLimit)
	atom := atomCost(re.root)
	c := &Contract{MaxInput: int(length)}
	c.Matcher = re.matcherContract(length, atom)
	if re.prog != nil && re.flags&NoSub == 0 {
		if re.onePass {
			walk := re.onePassContract(length, atom)
			c.OnePass = &walk
		}
		solver := re.solverContract(length, atom)
		c.Solver = &solver
	}
	return c
}

// matcherContract bounds phase A.
// length is the subject bound in bytes, and it also bounds the character count.
func (re *Regexp) matcherContract(length, atom int64) BackendContract {
	if re.prog == nil {
		// The expanded program passed the size cap.
		// Exec then only counts the subject characters against the minimum length, once in bytes and once in characters.
		// It can allocate one error value.
		return BackendContract{HeapBytes: errorBytes,
			StackBytes: matcherStackBytes,
			Steps:      cAdd(cMul(2, length), 2)}
	}
	n := int64(len(re.prog.ins))
	k := int64(re.minSlots)
	ring := int64(2)
	if re.prog.multi {
		ring = maxElemAhead + 1
	}

	// The pooled workspace, plus one possible error value.
	heap := cAdd(workspaceHeapBound(n, k, ring), errorBytes)

	// One boundary filters the live list, drains the closure, and runs the consuming transitions.
	// Epsilon relaxation can revisit an instruction once per distinct payload.
	// Payloads pass through the closure unchanged, so their count is the live thread count plus the spawn.
	// With no counters, a payload is a start offset alone.
	payloads := n + 1
	if k == 0 {
		payloads = min(n+1, cAdd(length, 2))
	}
	perPop := cMul(2, k+3)
	perBoundary := cAdd(cMul(n, cMul(payloads, perPop)),
		cMul(n, cAdd(atom, k+8)))
	steps := cMul(cAdd(length, 2), perBoundary)

	// A multi-character equivalence test recurses per element character.
	stack := int64(matcherStackBytes) + equivFrames*frameBytes

	return BackendContract{HeapBytes: heap, StackBytes: stack,
		Steps: steps}
}

// captureHeap counts the allocations every capture call performs before its backend runs.
// Those allocations are the decoded window with its escaping struct, and the span buffer.
func (re *Regexp) captureHeap(length int64) int64 {
	window := cAdd(cMul(4, length), cMul(8, cAdd(length, 1)))
	return cAdd(window+64, cMul(matchBytes, int64(re.nsub)+1))
}

// onePassContract bounds the phase B capture walk.
// The walk allocates nothing itself, and it visits each pattern node at most once per span position.
// Each group visit also clears the groups nested inside it, so the group count joins the per-visit cost.
func (re *Regexp) onePassContract(length, atom int64) BackendContract {
	perVisit := cAdd(atom, int64(re.nsub)+1)
	return BackendContract{
		HeapBytes:  re.captureHeap(length),
		StackBytes: cMul(cAdd(astHeight(re.root), 4), frameBytes),
		Steps: cMul(cMul(astSize(re.root), cAdd(length, 2)),
			perVisit),
	}
}

// solverContract bounds the phase B memoized parse search.
func (re *Regexp) solverContract(length, atom int64) BackendContract {
	depth := solverDepth(re.root, length)
	structural := min(solverSteps(re.root, length),
		cAdd(capWorkLimit, depth))
	tree := treeNodes(re.root, length)

	// One counted step can test one atom, compare two candidate trees, and touch the counter vectors.
	// A tree comparison walks both trees in full, and the work counter does not see that walk.
	// The tree size bound pays for it instead.
	// Reading the groups from the winning parse clears nested groups per visited node, and that walk runs once.
	perStep := cAdd(atom, cAdd(cMul(2, tree),
		cMul(2, int64(re.minSlots))+4))
	steps := cAdd(cMul(structural, perStep),
		cMul(tree, int64(re.nsub)+2))

	// One counted step can allocate parse nodes, one child list, and one memo record.
	// The bump arenas hand out whole chunks, so one partly used chunk of each kind joins the bound.
	// The slack covers the solver struct, the map headers, and the chunk lists.
	perAlloc := cAdd(2*ptreeBytes+mapEntryBytes,
		cMul(8, cAdd(solverFanout(re.root, length), 1)))
	heap := cAdd(cMul(structural, perAlloc),
		cMul(16, int64(re.minSlots)))
	heap = cAdd(heap, arenaChunkSize*(ptreeBytes+8)+1024)
	heap = cAdd(heap, re.captureHeap(length))

	// This is the parse search recursion.
	// It adds the equivalence recursion that one multi-character bracket test can start below it.
	stack := cMul(cAdd(depth, equivFrames+4), frameBytes)

	return BackendContract{HeapBytes: heap, StackBytes: stack,
		Steps: steps}
}

// astSize returns the pattern node count.
func astSize(n *node) int64 {
	var total int64
	walk(n, func(*node) { total++ })
	return total
}

// astHeight returns the pattern tree height.
func astHeight(n *node) int64 {
	deepest := int64(0)
	for _, child := range n.ch {
		deepest = max(deepest, astHeight(child))
	}
	return deepest + 1
}

// atomCost bounds the operations one atom test can perform.
func atomCost(n *node) int64 {
	cost := int64(1)
	walk(n, func(m *node) {
		switch m.op {
		case opChar:
			cost = max(cost, int64(len(m.fold))+1)
		case opBracket:
			cost = max(cost, bracketAtomCost(m.br))
		}
	})
	return cost
}

// bracketAtomCost bounds one bracket membership test.
// The single test scans the member lists once per case preimage.
// A positive list with multi-character members also probes every candidate length.
// Under ICase, the equivalence test enumerates the preimages of each position.
func bracketAtomCost(b *bracketSet) int64 {
	// bracketFixedChecks pays for the fixed parts of one membership test.
	// Those parts are the sixteen class bits and the negation check.
	const bracketFixedChecks = 17
	members := int64(len(b.ranges)+len(b.elems)+len(b.equivs)) +
		bracketFixedChecks
	cost := cMul(maxPreimages+1, members)
	if !b.hasMultiMembers() {
		return cost
	}
	var elemChars int64
	for _, e := range b.elems {
		elemChars += int64(len(e))
	}
	multi := elemChars
	if len(b.equivs) > 0 {
		candidates := int64(1)
		if b.icase {
			for range maxElemAhead {
				candidates *= maxPreimages + 1
			}
		}
		multi = cAdd(multi,
			cMul(candidates, cMul(int64(len(b.equivs))+1, maxElemAhead)))
	}
	return cAdd(cost, cMul(maxElemAhead-1, multi))
}

// solverSteps bounds the counted work of the parse search over any span of at most length characters.
// Every node pays for its bestParse misses.
// Concatenations, repetitions, and alternations add their split and branch loops per memo state.
func solverSteps(n *node, length int64) int64 {
	spans := cMul(cAdd(length, 1), cAdd(length, 2)) / 2
	total := spans
	switch n.op {
	case opConcat:
		total = cAdd(total, cMul(int64(len(n.ch)-1),
			cMul(spans, cAdd(length, 2))))
	case opAlt:
		total = cAdd(total, cMul(int64(len(n.ch)), spans))
	case opRepeat:
		states := int64(n.min) + 1
		if n.max != infinite {
			states = int64(n.max) + 1
		}
		total = cAdd(total, cMul(cMul(2, states),
			cMul(spans, cAdd(length, 2))))
	}
	for _, child := range n.ch {
		total = cAdd(total, solverSteps(child, length))
	}
	return total
}

// repInstances bounds the instance count of one repetition over a span of at most length characters.
// Null instances stop at the minimum, so the count passes the span by at most the minimum plus one.
func repInstances(n *node, length int64) int64 {
	instances := cAdd(length, int64(n.min)+1)
	if n.max != infinite {
		instances = min(instances, int64(n.max))
	}
	return instances
}

// solverDepth bounds the recursion of the parse search.
// A repetition chains one frame per instance, and a concatenation chains one frame per child.
// Sibling calls unwind before the next one starts, so the bounds add along one root path only.
func solverDepth(n *node, length int64) int64 {
	switch n.op {
	case opGroup, opAlt, opConcat:
		deepest := int64(0)
		for _, child := range n.ch {
			deepest = max(deepest, solverDepth(child, length))
		}
		if n.op == opConcat {
			return cAdd(deepest, int64(len(n.ch))+1)
		}
		return deepest + 1
	case opRepeat:
		return cAdd(repInstances(n, length)+1,
			solverDepth(n.ch[0], length))
	}
	return 1
}

// treeNodes bounds the node count of one parse tree over a span of at most length characters.
// Nested nullable repetitions with a positive minimum multiply.
// The bound then saturates, and the contract reports figures too large to accept.
func treeNodes(n *node, length int64) int64 {
	switch n.op {
	case opGroup, opAlt:
		widest := int64(0)
		for _, child := range n.ch {
			widest = max(widest, treeNodes(child, length))
		}
		return cAdd(widest, 1)
	case opConcat:
		total := int64(1)
		for _, child := range n.ch {
			total = cAdd(total, treeNodes(child, length))
		}
		return total
	case opRepeat:
		return cAdd(1, cMul(repInstances(n, length),
			treeNodes(n.ch[0], length)))
	}
	return 1
}

// solverFanout bounds the widest child list one parse node can hold.
func solverFanout(n *node, length int64) int64 {
	widest := int64(1)
	switch n.op {
	case opConcat:
		widest = int64(len(n.ch))
	case opRepeat:
		widest = cAdd(repInstances(n, length), 1)
	}
	for _, child := range n.ch {
		widest = max(widest, solverFanout(child, length))
	}
	return widest
}
