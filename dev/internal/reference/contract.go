package reference

// This file computes resource contracts.
// A contract bounds, for each backend, what one Exec call can use on a subject of a given maximum length.
// An application can compare the figures against its budget and refuse a pattern before it ever runs.
//
// Heap figures count explicit allocation requests with fixed 64-bit field sizes and conservative allowances.
// They are therefore the same on every platform.
// Capture figures include allocator rounding for the three short-lived buffers.
// Runtime object headers, general allocator metadata, map buckets, and garbage-collector bookkeeping stay outside
// this portable model.
// Stack figures multiply the deepest possible call chain by a fixed per-frame estimate.
// Step figures count abstract unit-cost operations, not nanoseconds.
// They are worst-case bounds, and ordinary subjects stay far below them.

import (
	"math/bits"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

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
//     It is then the only phase B backend that can run.
//   - Solver is the phase B memoized parse search.
//     It is set when parenthesized subexpression offsets require the general parse search.
//     OnePass and Solver are mutually exclusive.
//
// Every value saturates at 1<<62, which marks a bound too large to be useful.
// The pooled per-Regexp workspace counts once.
// Concurrent Exec calls on the same Regexp each use their own copy of it.
// Each capture backend includes the shared window allocations.
// HeapBytes combines the matcher with the selected capture backend.
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
	// singleLookupFrames is the call depth below a single-character bracket test.
	singleLookupFrames = 8
	// multiLookupFrames is the call depth below a multi-character probe, which recurses once per lookahead
	// character.
	multiLookupFrames = maxElemAhead + 8
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
// It adds the matcher workspace and the selected capture backend.
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
// It combines the matcher with the selected capture backend.
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
	if re.prog != nil && re.nsub > 0 && re.flags&NoSub == 0 {
		if re.onePass {
			walk := re.onePassContract(length, atom)
			c.OnePass = &walk
		} else {
			solver := re.solverContract(length, atom)
			c.Solver = &solver
		}
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

	// A bracket probe can start the deepest lookup below the fixed frames.
	stack := int64(matcherStackBytes) + multiLookupFrames*frameBytes

	return BackendContract{HeapBytes: heap, StackBytes: stack,
		Steps: steps}
}

// captureHeap bounds the allocations every capture call performs before its backend runs.
// It counts the decoded window, its escaping struct, and the span buffer, then adds an allowance for allocator rounding.
func (re *Regexp) captureHeap(length int64) int64 {
	window := cAdd(cMul(4, length), cMul(8, cAdd(length, 1)))
	spans := cMul(matchBytes, cAdd(int64(re.nsub), 1))
	payload := cAdd(window, spans)
	// Go's size-class gaps are below 8 KiB, and large allocations round to 8 KiB pages.
	// One page per buffer therefore covers the rounding whatever the subject length is.
	allowance := int64(3 * 8192)
	return cAdd(cAdd(payload, allowance), 64)
}

// onePassContract bounds the phase B capture walk.
// The walk allocates nothing itself.
// Its structural factor covers recursive visits and the fixed scans around them, including both concat scans.
// Each group visit also clears the groups nested inside it, so the group count joins the per-visit cost.
func (re *Regexp) onePassContract(length, atom int64) BackendContract {
	perVisit := cAdd(atom, int64(re.nsub)+1)
	return BackendContract{
		HeapBytes:  re.captureHeap(length),
		StackBytes: cMul(cAdd(astHeight(re.root), 2+singleLookupFrames), frameBytes),
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
	// Two entry frames sit above the parse search, and a span test can start a probe below it.
	stack := cMul(cAdd(depth, 3+multiLookupFrames), frameBytes)

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

// The bracket figures follow go/contract.go: loop-meter units of the engine, one helper per engine function.

// searchSteps bounds the probes of a binary search over count entries.
func searchSteps(count int) int64 {
	return int64(bits.Len(uint(count)))
}

const profileRowCost = 13

func u32ContainsCost(count int) int64 {
	return 1 + 3*searchSteps(count)
}

func findPairCost(count int) int64 {
	return 3 + 3*searchSteps(count)
}

func findCaseCost(count int) int64 {
	return 6 + 3*searchSteps(count)
}

func pairSourcesRunCost(count int, preimages int64) int64 {
	return 5 + 3*searchSteps(count) + 5*preimages
}

func compareSequenceCost(length int64) int64 {
	return 5 + 3*length
}

// lookupCosts holds the prices of the locale lookups a bracket test can call, one per engine function it names.
type lookupCosts struct {
	sequenceSearch int64
	contraction    int64
	primaryToken   int64
	classMask      int64
	casePreimages  int64
	caseConvert    int64
	// preimages is the most case preimages one character can have.
	preimages int64
}

// localeLookupCosts derives the lookup prices from the tables of one locale.
func localeLookupCosts(loc locale.Locale) lookupCosts {
	if loc.IsPOSIX() {
		return lookupCosts{contraction: 1, classMask: 3, casePreimages: 2, caseConvert: 2, preimages: 1}
	}
	t := loc.LookupTables()
	lc := lookupCosts{
		sequenceSearch: searchSteps(t.Sequences),
		contraction: 1 + profileRowCost + u32ContainsCost(t.ContractionAdds) + 1 +
			u32ContainsCost(t.RootContractions) + u32ContainsCost(t.ContractionRemoves),
		primaryToken: 1 + profileRowCost + findPairCost(t.Overrides) + 1 +
			findPairCost(t.RootEquivalences),
		classMask:   4,
		preimages:   int64(t.MaxPreimages),
		caseConvert: 2 + findCaseCost(t.CaseDefault),
	}
	if t.Turkic {
		lc.caseConvert += findCaseCost(t.CaseTurkic)
	}
	lc.casePreimages = 2 + pairSourcesRunCost(t.InverseUpper, lc.preimages) +
		pairSourcesRunCost(t.InverseLower, lc.preimages) + lc.preimages*(2+lc.preimages)
	return lc
}

func elementIDCost(lc *lookupCosts, length int64) int64 {
	if length == 1 {
		return 3
	}
	return 2 + 2*length + lc.sequenceSearch*(1+compareSequenceCost(length))
}

func collatingElementIDCost(lc *lookupCosts, length int64) int64 {
	cost := 1 + elementIDCost(lc, length)
	if length > 1 {
		cost += lc.contraction
	}
	return cost
}

func primaryEqualCost(lc *lookupCosts, left, right int64) int64 {
	return 1 + collatingElementIDCost(lc, left) + collatingElementIDCost(lc, right) + 2*lc.primaryToken
}

// equivsCost prices the comparison of a sequence of length characters with every equivalence class.
func equivsCost(b *bracketSet, lc *lookupCosts, length int64) int64 {
	cost := int64(0)
	for _, e := range b.equivs {
		cost = cAdd(cost, 1+primaryEqualCost(lc, length, int64(len(e))))
	}
	return cost
}

func positiveSingleCost(b *bracketSet, lc *lookupCosts) int64 {
	cost := 2 + searchSteps(len(b.ranges))
	if b.classMask != 0 {
		cost += lc.classMask
	}
	return cAdd(cost, equivsCost(b, lc, 1))
}

func matchesOneCost(b *bracketSet, lc *lookupCosts) int64 {
	positive := positiveSingleCost(b, lc)
	cost := cAdd(1, positive)
	if b.icase {
		cost = cAdd(cost, cAdd(lc.casePreimages, cMul(lc.preimages, cAdd(1, positive))))
	}
	return cost
}

// candidateLeafCost prices the membership test of one candidate sequence of the equivalence search.
func candidateLeafCost(b *bracketSet, lc *lookupCosts, length int64) int64 {
	return cAdd(1+collatingElementIDCost(lc, length), equivsCost(b, lc, length))
}

// probeCost prices one multi-character probe over a lookahead of length characters.
func probeCost(b *bracketSet, lc *lookupCosts, length int64) int64 {
	cost := cAdd(2, int64(len(b.elems)))
	counterpart := int64(1)
	if b.icase {
		counterpart = 1 + 2*lc.caseConvert
	}
	for _, e := range b.elems {
		if int64(len(e)) == length {
			cost = cAdd(cost, cMul(length, counterpart))
		}
	}
	if len(b.equivs) == 0 {
		return cost
	}
	leaf := candidateLeafCost(b, lc, length)
	if !b.icase {
		return cAdd(cost, cAdd(length+1, leaf))
	}
	// ICase tries every case candidate of the lookahead, so the recursion has (preimages+1)^length leaves.
	candidates := int64(1)
	for range length {
		candidates = cMul(candidates, lc.preimages+1)
	}
	return cAdd(cost, cMul(candidates, cAdd(2+lc.preimages, cAdd(lc.casePreimages, leaf))))
}

// bracketAtomCost bounds the work of one live bracket instruction at one boundary: the single-character test
// and the multi-character probes.
func bracketAtomCost(b *bracketSet) int64 {
	lc := localeLookupCosts(b.loc)
	cost := matchesOneCost(b, &lc)
	if b.multiLens == 0 {
		return cost
	}
	cost = cAdd(cost, maxElemAhead-1)
	for length := 2; length <= maxElemAhead; length++ {
		if b.multiLens&(1<<length) != 0 {
			cost = cAdd(cost, probeCost(b, &lc, int64(length)))
		}
	}
	return cost
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
