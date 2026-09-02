package revera

// This file computes resource contracts.
// A contract bounds, for each backend, what one Exec call can use on a subject of a given maximum length.
// An application can compare the figures against its budget and refuse a pattern before it ever runs.
//
// Heap figures count the explicit allocations the code performs, with fixed 64-bit field sizes.
// They are therefore the same on every platform.
// The per-record constants absorb buffer growth by doubling and hash-table load factors.
// The true footprint stays a bounded constant factor below the figure.
// Stack figures multiply the deepest possible call chain by a fixed per-frame estimate.
// Step figures count abstract unit-cost operations, not nanoseconds.
// They are worst-case bounds, and ordinary subjects stay far below them.

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
//     HasOnePass is set when compilation proved that every span has one parse.
//     Its figures apply when the walk succeeds, which the proof guarantees.
//   - Solver is the phase B memoized parse search.
//     It is the guaranteed ceiling for any call that fills group offsets.
//     The walk falls back to it on an inconsistency.
//     HasSolver is false when the pattern was compiled with FlagNoSub, and when Exec cannot reach phase B.
//
// Every value saturates at 1<<62, which marks a bound too large to be useful.
// The per-call workspace counts once.
// Both capture backends include the shared window allocations, so nothing may sum their heap fields.
// ContractHeapBytes takes the maximum instead.
type Contract struct {
	MaxInput   int
	Matcher    BackendContract
	OnePass    BackendContract
	HasOnePass bool
	Solver     BackendContract
	HasSolver  bool
}

// contractCap saturates contract arithmetic.
// It leaves room to add saturated values without overflow.
const contractCap = int64(1) << 62

// Fixed 64-bit sizes of the records the contract counts, in bytes.
// The values absorb growth by append doubling, the load factor of the memo tables, and the rehash transient.
const (
	// frameBytes estimates one recursive call frame.
	frameBytes = 256
	// matcherStackBytes covers the fixed frames of the iterative phase A executor.
	matcherStackBytes = 2048
	// equivFrames bounds the equivCandidate recursion.
	// It allows one frame per character of a multi-character collating element, plus slack.
	equivFrames = maxElemAhead + 2
	// ptreeBytes is one solver parse-tree record of 24 bytes, with arena doubling folded in.
	ptreeBytes = 48
	// kidBytes is one child-list entry of 4 bytes, with doubling folded in.
	kidBytes = 8
	// mapEntryBytes is one memo record: a 16-byte key, a 12-byte value, and a mark.
	// It assumes a 3/4 load and folds in the rehash transient.
	mapEntryBytes = 128
	// matchBytes is the size of one Match.
	matchBytes = 16
	// bracketFixedChecks pays for the fixed parts of one bracket membership test.
	// Those parts are the sixteen class bits and the negation check.
	bracketFixedChecks = 17
)

func cAdd(a int64, b int64) int64 {
	if a > contractCap-b {
		return contractCap
	}
	return a + b
}

func cMul(a int64, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > contractCap/b {
		return contractCap
	}
	return a * b
}

// ContractHeapBytes bounds the heap of one whole call.
// It adds the matcher workspace and the most expensive capture backend that can run.
func ContractHeapBytes(c *Contract) int64 {
	capture := int64(0)
	if c.HasOnePass {
		capture = c.OnePass.HeapBytes
	}
	if c.HasSolver {
		capture = max(capture, c.Solver.HeapBytes)
	}
	return cAdd(c.Matcher.HeapBytes, capture)
}

// ContractStackBytes bounds the stack of one whole call.
func ContractStackBytes(c *Contract) int64 {
	deepest := c.Matcher.StackBytes
	if c.HasOnePass {
		deepest = max(deepest, c.OnePass.StackBytes)
	}
	if c.HasSolver {
		deepest = max(deepest, c.Solver.StackBytes)
	}
	return deepest
}

// ContractSteps bounds the operations of one whole call.
// The walk can fall back to the solver, so both capture backends count.
func ContractSteps(c *Contract) int64 {
	steps := c.Matcher.Steps
	if c.HasOnePass {
		steps = cAdd(steps, c.OnePass.Steps)
	}
	if c.HasSolver {
		steps = cAdd(steps, c.Solver.Steps)
	}
	return steps
}

// ContractFor returns the resource contract of one compiled expression for subjects of at most maxInput bytes.
// A larger maxInput clamps to the subject limit of Exec.
func ContractFor(re *Regexp, maxInput int) Contract {
	length := int64(min(max(maxInput, 0), subjectLimit))
	atom := atomCost(re)
	var c Contract
	c.MaxInput = int(length)
	c.Matcher = matcherContract(re, length, atom)
	if re.progOK && re.flags&FlagNoSub == 0 {
		if re.onePass {
			c.OnePass = onePassContract(re, length, atom)
			c.HasOnePass = true
		}
		c.Solver = solverContract(re, length, atom)
		c.HasSolver = true
	}
	return c
}

// matcherContract bounds phase A.
// length is the subject bound in bytes, and it also bounds the character count.
func matcherContract(re *Regexp, length int64, atom int64) BackendContract {
	var b BackendContract
	if !re.progOK {
		// The expanded program passed the size cap.
		// Exec then only counts the subject characters against the minimum length, once in bytes and once in characters.
		// It allocates nothing.
		b.StackBytes = matcherStackBytes
		b.Steps = cAdd(cMul(2, length), 2)
		return b
	}
	n := int64(len(re.prog.ins))
	k := int64(re.minSlots)
	ring := int64(2)
	if re.prog.multi {
		ring = maxElemAhead + 1
	}

	// The per-call workspace and everything that grows in it.
	heap := workspaceHeapBound(n, k, ring)

	// The step figure is stepsFigure of lean/Vego/PhaseA.lean, which lean/Vego/PhaseAProofs.lean proves for
	// every program and subject.
	// One boundary pays for its closure, priced by a potential of (n+1)^2 units at weight 4k+22 each, which
	// covers every pop, every relaxation and every compaction; for its consuming transitions, one test and
	// up to ring-1 arrivals per live instruction; and for its fixed work.
	weight := cAdd(cMul(4, k), 22)
	perTest := cAdd(cAdd(atom, 2), cMul(ring-1, cAdd(cMul(4, k), 12)))
	perBoundary := cMul(weight, cMul(n+1, n+1))
	perBoundary = cAdd(perBoundary, cMul(n, perTest))
	perBoundary = cAdd(perBoundary, cAdd(cAdd(n, cMul(2, k)), cAdd(cMul(4, ring), 38)))
	steps := cAdd(cAdd(24+ring, length), cMul(cAdd(length, 1), perBoundary))

	// A multi-character equivalence test recurses once per element character.
	stack := int64(matcherStackBytes) + equivFrames*frameBytes

	b.HeapBytes = heap
	b.StackBytes = stack
	b.Steps = steps
	return b
}

// captureHeap counts the allocations every capture call performs before its backend runs.
// Those allocations are the decoded window and the span buffer.
func captureHeap(re *Regexp, length int64) int64 {
	window := cAdd(cMul(4, length), cMul(8, cAdd(length, 1)))
	return cAdd(window+64, cMul(matchBytes, int64(re.nsub)+1))
}

// onePassContract bounds the phase B capture walk.
// The walk allocates nothing itself, and it visits each pattern node at most once per span position.
// Each group visit also clears the groups nested inside it, so the group count joins the per-visit cost.
func onePassContract(re *Regexp, length int64, atom int64) BackendContract {
	var b BackendContract
	perVisit := cAdd(atom, int64(re.nsub)+1)
	b.HeapBytes = captureHeap(re, length)
	b.StackBytes = cMul(cAdd(astHeight(re.nodes, re.root), 4), frameBytes)
	b.Steps = cMul(cMul(astSize(re.nodes, re.root), cAdd(length, 2)),
		perVisit)
	return b
}

// solverContract bounds the phase B memoized parse search.
func solverContract(re *Regexp, length int64, atom int64) BackendContract {
	var b BackendContract
	depth := solverDepth(re.nodes, re.root, length)
	structural := min(solverSteps(re.nodes, re.root, length),
		cAdd(capWorkLimit, depth))
	tree := treeNodes(re.nodes, re.root, length)

	// One counted step can test one atom, compare two candidate trees, and touch the counter vectors.
	// A tree comparison walks both trees in full, and the work counter does not see that walk.
	// The tree size bound pays for it instead.
	// Reading the groups from the winning parse clears nested groups per visited node, and that walk runs once.
	perStep := cAdd(atom, cAdd(cMul(2, tree),
		cMul(2, int64(re.minSlots))+4))
	steps := cAdd(cMul(structural, perStep),
		cMul(tree, int64(re.nsub)+2))

	// One counted step can allocate parse records, one child list, and one memo record.
	// The slack term covers the solver struct, the counter vectors, and the initial table headers.
	perAlloc := cAdd(2*ptreeBytes+mapEntryBytes,
		cMul(kidBytes, cAdd(solverFanout(re.nodes, re.root, length), 1)))
	heap := cAdd(cMul(structural, perAlloc),
		cMul(16, int64(re.minSlots)))
	heap = cAdd(heap, 4096)
	heap = cAdd(heap, captureHeap(re, length))

	// This is the parse search recursion.
	// It adds the equivalence recursion that one multi-character bracket test can start below it.
	stack := cMul(cAdd(depth, equivFrames+4), frameBytes)

	b.HeapBytes = heap
	b.StackBytes = stack
	b.Steps = steps
	return b
}

// astSize returns the pattern node count under ni.
func astSize(nodes []node, ni int32) int64 {
	total := int64(1)
	for i := 0; i < len(nodes[ni].ch); i++ {
		total = cAdd(total, astSize(nodes, nodes[ni].ch[i]))
	}
	return total
}

// astHeight returns the pattern tree height under ni.
func astHeight(nodes []node, ni int32) int64 {
	deepest := int64(0)
	for i := 0; i < len(nodes[ni].ch); i++ {
		deepest = max(deepest, astHeight(nodes, nodes[ni].ch[i]))
	}
	return deepest + 1
}

// atomCost bounds the operations one atom test can perform.
func atomCost(re *Regexp) int64 {
	return atomCostNode(re.nodes, re.brackets, re.root)
}

func atomCostNode(nodes []node, brs []bracketSet, ni int32) int64 {
	cost := int64(1)
	switch nodes[ni].op {
	case opChar:
		cost = int64(len(nodes[ni].fold)) + 1
	case opBracket:
		cost = bracketAtomCost(brs, nodes[ni].br)
	}
	for i := 0; i < len(nodes[ni].ch); i++ {
		cost = max(cost, atomCostNode(nodes, brs, nodes[ni].ch[i]))
	}
	return cost
}

// bracketAtomCost bounds one bracket membership test.
// The single test scans the member lists once per case preimage.
// A positive list with multi-character members also probes every candidate length.
// Under ICase, the equivalence test enumerates the preimages of each position.
func bracketAtomCost(brs []bracketSet, bi int32) int64 {
	members := int64(len(brs[bi].ranges)+len(brs[bi].elems)+
		len(brs[bi].equivs)) + bracketFixedChecks
	cost := cMul(maxPreimages+1, members)
	if !bracketHasMultiMembers(brs, bi) {
		return cost
	}
	var elemChars int64
	for i := 0; i < len(brs[bi].elems); i++ {
		elemChars += int64(len(brs[bi].elems[i]))
	}
	multi := elemChars
	if len(brs[bi].equivs) > 0 {
		candidates := int64(1)
		if brs[bi].icase {
			for i := 0; i < maxElemAhead; i++ {
				candidates *= maxPreimages + 1
			}
		}
		multi = cAdd(multi,
			cMul(candidates, cMul(int64(len(brs[bi].equivs))+1, maxElemAhead)))
	}
	return cAdd(cost, cMul(maxElemAhead-1, multi))
}

// solverSteps bounds the counted work of the parse search over any span of at most length characters.
// Every node pays for its bestParse misses.
// Concatenations, repetitions, and alternations add their split and branch loops per memo state.
func solverSteps(nodes []node, ni int32, length int64) int64 {
	spans := cMul(cAdd(length, 1), cAdd(length, 2)) / 2
	total := spans
	switch nodes[ni].op {
	case opConcat:
		total = cAdd(total, cMul(int64(len(nodes[ni].ch)-1),
			cMul(spans, cAdd(length, 2))))
	case opAlt:
		total = cAdd(total, cMul(int64(len(nodes[ni].ch)), spans))
	case opRepeat:
		states := int64(nodes[ni].min) + 1
		if nodes[ni].max != infinite {
			states = int64(nodes[ni].max) + 1
		}
		total = cAdd(total, cMul(cMul(2, states),
			cMul(spans, cAdd(length, 2))))
	}
	for i := 0; i < len(nodes[ni].ch); i++ {
		total = cAdd(total, solverSteps(nodes, nodes[ni].ch[i], length))
	}
	return total
}

// repInstances bounds the instance count of one repetition over a span of at most length characters.
// Null instances stop at the minimum, so the count passes the span by at most the minimum plus one.
func repInstances(nodes []node, ni int32, length int64) int64 {
	instances := cAdd(length, int64(nodes[ni].min)+1)
	if nodes[ni].max != infinite {
		instances = min(instances, int64(nodes[ni].max))
	}
	return instances
}

// solverDepth bounds the recursion of the parse search.
// A repetition chains one frame per instance, and a concatenation chains one frame per child.
// Sibling calls unwind before the next one starts, so the bounds add along one root path only.
func solverDepth(nodes []node, ni int32, length int64) int64 {
	switch nodes[ni].op {
	case opGroup, opAlt, opConcat:
		deepest := int64(0)
		for i := 0; i < len(nodes[ni].ch); i++ {
			deepest = max(deepest, solverDepth(nodes, nodes[ni].ch[i], length))
		}
		if nodes[ni].op == opConcat {
			return cAdd(deepest, int64(len(nodes[ni].ch))+1)
		}
		return deepest + 1
	case opRepeat:
		return cAdd(repInstances(nodes, ni, length)+1,
			solverDepth(nodes, nodes[ni].ch[0], length))
	}
	return 1
}

// treeNodes bounds the node count of one parse tree over a span of at most length characters.
// Nested nullable repetitions with a positive minimum multiply.
// The bound then saturates, and the contract reports figures too large to accept.
func treeNodes(nodes []node, ni int32, length int64) int64 {
	switch nodes[ni].op {
	case opGroup, opAlt:
		widest := int64(0)
		for i := 0; i < len(nodes[ni].ch); i++ {
			widest = max(widest, treeNodes(nodes, nodes[ni].ch[i], length))
		}
		return cAdd(widest, 1)
	case opConcat:
		total := int64(1)
		for i := 0; i < len(nodes[ni].ch); i++ {
			total = cAdd(total, treeNodes(nodes, nodes[ni].ch[i], length))
		}
		return total
	case opRepeat:
		return cAdd(1, cMul(repInstances(nodes, ni, length),
			treeNodes(nodes, nodes[ni].ch[0], length)))
	}
	return 1
}

// solverFanout bounds the widest child list one parse record can hold.
func solverFanout(nodes []node, ni int32, length int64) int64 {
	widest := int64(1)
	switch nodes[ni].op {
	case opConcat:
		widest = int64(len(nodes[ni].ch))
	case opRepeat:
		widest = cAdd(repInstances(nodes, ni, length), 1)
	}
	for i := 0; i < len(nodes[ni].ch); i++ {
		widest = max(widest, solverFanout(nodes, nodes[ni].ch[i], length))
	}
	return widest
}
