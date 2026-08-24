package revera

// This file holds the phase B capture solver. Phase A fixed the match
// span; this solver finds the best parse of that span under the selection
// order, then reads the group spans from the winning parse.
//
// Sibling segments of the comparison vector are independent, so the best
// parse of a node over a span does not depend on its context, and
// memoization per (node, span) is valid.

type concatKey struct {
	n    *node
	idx  int
	i, j int
}

type repKey struct {
	n        *node
	i, j     int
	done     int
	hasEmpty bool
}

type capSolver struct {
	re     *Regexp
	d      *decoded
	eflags ExecFlags
	memo   map[memoKey]*ptree
	seen   map[memoKey]bool
	cmemo  map[concatKey][]*ptree
	cseen  map[concatKey]bool
	rmemo  map[repKey][]*ptree
	rseen  map[repKey]bool
	ctrA   []int
	ctrB   []int
	work   int
	failed bool

	// Bump arenas for parse-tree nodes and child lists. Chunks survive
	// pooled reuse; the cleared maps make stale entries unreachable.
	treeChunks [][]ptree
	treeChunk  int
	kidChunks  [][]*ptree
	kidChunk   int
}

const arenaChunkSize = 256

// newTree allocates one parse-tree node from the arena.
func (s *capSolver) newTree(t ptree) *ptree {
	if s.treeChunk == len(s.treeChunks) {
		s.treeChunks = append(s.treeChunks, make([]ptree, 0, arenaChunkSize))
	}
	chunk := &s.treeChunks[s.treeChunk]
	if len(*chunk) == cap(*chunk) {
		s.treeChunk++
		return s.newTree(t)
	}
	*chunk = append(*chunk, t)
	return &(*chunk)[len(*chunk)-1]
}

// allocKids returns a child list of the given length from the arena.
func (s *capSolver) allocKids(length int) []*ptree {
	if length > arenaChunkSize {
		return make([]*ptree, length)
	}
	if s.kidChunk == len(s.kidChunks) {
		s.kidChunks = append(s.kidChunks, make([]*ptree, 0, arenaChunkSize))
	}
	chunk := &s.kidChunks[s.kidChunk]
	if cap(*chunk)-len(*chunk) < length {
		s.kidChunk++
		return s.allocKids(length)
	}
	start := len(*chunk)
	*chunk = (*chunk)[:start+length]
	return (*chunk)[start : start+length : start+length]
}

func (s *capSolver) resetArenas() {
	for i := range s.treeChunks {
		s.treeChunks[i] = s.treeChunks[i][:0]
	}
	s.treeChunk = 0
	for i := range s.kidChunks {
		s.kidChunks[i] = s.kidChunks[i][:0]
	}
	s.kidChunk = 0
}

// capWorkLimit bounds the polynomial parse search. Reaching it reports
// ESpace instead of looping for a very long time.
const capWorkLimit = 50_000_000

func (s *capSolver) step() bool {
	s.work++
	if s.work > capWorkLimit {
		s.failed = true
	}
	return !s.failed
}

// cmpCand compares two parses of the same pattern node over the same
// span: minimal repetition counters first, then structure. A negative
// result means a wins.
func (s *capSolver) cmpCand(a, b *ptree) int {
	if s.re.minSlots > 0 {
		ca := s.ctrA[:s.re.minSlots]
		cb := s.ctrB[:s.re.minSlots]
		clear(ca)
		clear(cb)
		addCountersInt(a, ca)
		addCountersInt(b, cb)
		for idx := range s.re.minSlots {
			if ca[idx] != cb[idx] {
				return ca[idx] - cb[idx]
			}
		}
	}
	return structCmp(a, b)
}

func addCountersInt(t *ptree, out []int) {
	if t.n.op == opRepeat && t.n.minimal {
		out[t.n.index] += t.j - t.i
	}
	for _, kid := range t.kids {
		addCountersInt(kid, out)
	}
}

// bestParse returns the best parse of n over exactly [i, j), or nil.
func (s *capSolver) bestParse(n *node, i, j int) *ptree {
	// A saturated maxL means unbounded, never an actual limit.
	if span := j - i; span < n.minL || (n.maxL < lenInf && span > n.maxL) {
		return nil
	}
	key := memoKey{n, i, j}
	if s.seen[key] {
		return s.memo[key]
	}
	if !s.step() {
		return nil
	}
	var best *ptree
	switch n.op {
	case opChar:
		if j == i+1 && s.re.charMatches(n, s.d.runes[i]) {
			best = s.newTree(ptree{n: n, i: i, j: j})
		}
	case opAny:
		if j == i+1 && s.re.anyMatches(s.d.runes[i]) {
			best = s.newTree(ptree{n: n, i: i, j: j})
		}
	case opBracket:
		if s.bracketSpanOK(n, i, j) {
			best = s.newTree(ptree{n: n, i: i, j: j})
		}
	case opBOL:
		if j == i && s.re.atBOL(s.d, i, s.eflags) {
			best = s.newTree(ptree{n: n, i: i, j: j})
		}
	case opEOL:
		if j == i && s.re.atEOL(s.d, i, s.eflags) {
			best = s.newTree(ptree{n: n, i: i, j: j})
		}
	case opGroup:
		if sub := s.bestParse(n.ch[0], i, j); sub != nil {
			kids := s.allocKids(1)
			kids[0] = sub
			best = s.newTree(ptree{n: n, i: i, j: j, kids: kids})
		}
	case opAlt:
		for bi, branch := range n.ch {
			sub := s.bestParse(branch, i, j)
			if sub == nil {
				continue
			}
			kids := s.allocKids(1)
			kids[0] = sub
			candidate := s.newTree(ptree{n: n, i: i, j: j, branch: bi,
				kids: kids})
			if best == nil || s.cmpCand(candidate, best) < 0 {
				best = candidate
			}
		}
	case opConcat:
		kids := s.bestConcat(n, 0, i, j)
		if kids != nil {
			best = s.newTree(ptree{n: n, i: i, j: j, kids: kids})
		}
	case opRepeat:
		if i == j && n.min == 0 {
			// Mirror the oracle: one null occurrence when the operand
			// has a null match and the maximum is not zero.
			var sub *ptree
			if n.max != 0 {
				sub = s.bestParse(n.ch[0], i, i)
			}
			if sub != nil {
				kids := s.allocKids(1)
				kids[0] = sub
				best = s.newTree(ptree{n: n, i: i, j: j, kids: kids})
			} else {
				best = s.newTree(ptree{n: n, i: i, j: j})
			}
			break
		}
		kids, ok := s.bestRep(n, i, j, 0, false)
		if ok {
			best = s.newTree(ptree{n: n, i: i, j: j, kids: kids})
		}
	}
	s.seen[key] = true
	s.memo[key] = best
	return best
}

func (s *capSolver) bracketSpanOK(n *node, i, j int) bool {
	k := j - i
	if k < 1 {
		return false
	}
	newlineMode := s.re.flags&Newline != 0
	icase := s.re.flags&ICase != 0
	if k == 1 {
		return n.br.matchesOne(s.d.runes[i], s.re.loc, icase, newlineMode)
	}
	if !n.br.hasMultiMembers() {
		return false
	}
	return n.br.matchesMulti(s.d.runes[i:j], s.re.loc, icase)
}

// bestConcat returns the best child parses of n.ch[idx:] covering [i, j),
// or nil when none exists.
func (s *capSolver) bestConcat(n *node, idx, i, j int) []*ptree {
	if idx == len(n.ch)-1 {
		sub := s.bestParse(n.ch[idx], i, j)
		if sub == nil {
			return nil
		}
		kids := s.allocKids(1)
		kids[0] = sub
		return kids
	}
	key := concatKey{n, idx, i, j}
	if s.cseen[key] {
		return s.cmemo[key]
	}
	if !s.step() {
		return nil
	}
	var best []*ptree
	var bestTree *ptree
	head0 := n.ch[idx]
	lo := i + head0.minL
	if n.sufMax[idx+1] < lenInf {
		lo = max(lo, j-n.sufMax[idx+1])
	}
	hi := j - n.sufMin[idx+1]
	if head0.maxL < lenInf {
		hi = min(hi, i+head0.maxL)
	}
	for m := lo; m <= hi; m++ {
		if !s.step() {
			return nil
		}
		head := s.bestParse(head0, i, m)
		if head == nil {
			continue
		}
		tail := s.bestConcat(n, idx+1, m, j)
		if tail == nil {
			continue
		}
		kids := s.allocKids(len(tail) + 1)
		kids[0] = head
		copy(kids[1:], tail)
		candidate := s.newTree(ptree{n: n, i: i, j: j, kids: kids})
		if bestTree == nil || s.cmpCand(candidate, bestTree) < 0 {
			bestTree = candidate
			best = kids
		}
	}
	s.cseen[key] = true
	s.cmemo[key] = best
	return best
}

// bestRep returns the best remaining instance list of repetition n over
// [i, j) after done instances, or ok == false when none exists.
// The section 8.5 rule holds: a null instance is allowed only while the
// final count stays at the minimum.
func (s *capSolver) bestRep(n *node, i, j, done int, hasEmpty bool) ([]*ptree, bool) {
	// With no upper bound, behavior only depends on done through the
	// comparison with the minimum. Folding larger values onto the
	// minimum keeps the memoized state space quadratic in the span.
	if n.max == infinite && done > n.min {
		done = n.min
	}
	key := repKey{n, i, j, done, hasEmpty}
	if s.rseen[key] {
		result := s.rmemo[key]
		if result == nil {
			return nil, false
		}
		if len(result) == 1 && result[0] == nil {
			return nil, true
		}
		return result, true
	}
	if !s.step() {
		return nil, false
	}
	var best []*ptree
	found := false
	var bestTree *ptree
	tryCandidate := func(kids []*ptree) {
		candidate := s.newTree(ptree{n: n, i: i, j: j, kids: kids})
		if !found || s.cmpCand(candidate, bestTree) < 0 {
			best = kids
			bestTree = candidate
			found = true
		}
	}
	if i == j && done >= n.min && (!hasEmpty || done == n.min) {
		tryCandidate(nil)
	}
	canTake := n.max == infinite || done < n.max
	if canTake && !(hasEmpty && done >= n.min) {
		child := n.ch[0]
		// The remaining instances after this one bound the tail span.
		tailMin := satMul(max(n.min-done-1, 0), child.minL)
		tailMax := lenInf
		if n.max != infinite {
			tailMax = satMul(n.max-done-1, child.maxL)
		} else if child.maxL == 0 {
			tailMax = 0
		}
		lo := i + child.minL
		if tailMax < lenInf {
			lo = max(lo, j-tailMax)
		}
		hi := j - tailMin
		if child.maxL < lenInf {
			hi = min(hi, i+child.maxL)
		}
		for m := lo; m <= hi; m++ {
			if m == i && done >= n.min {
				continue
			}
			if !s.step() {
				return nil, false
			}
			head := s.bestParse(child, i, m)
			if head == nil {
				continue
			}
			tail, ok := s.bestRep(n, m, j, done+1, hasEmpty || m == i)
			if !ok {
				continue
			}
			kids := s.allocKids(len(tail) + 1)
			kids[0] = head
			copy(kids[1:], tail)
			tryCandidate(kids)
		}
	}
	s.rseen[key] = true
	if !found {
		s.rmemo[key] = nil
		return nil, false
	}
	if best == nil {
		s.rmemo[key] = []*ptree{nil}
		return nil, true
	}
	s.rmemo[key] = best
	return best, true
}

// getSolver reuses a pooled solver; the maps keep their buckets across
// executions and only need clearing.
func (re *Regexp) getSolver() *capSolver {
	if s, ok := re.capPool.Get().(*capSolver); ok {
		clear(s.memo)
		clear(s.seen)
		clear(s.cmemo)
		clear(s.cseen)
		clear(s.rmemo)
		clear(s.rseen)
		s.work = 0
		s.failed = false
		s.resetArenas()
		return s
	}
	return &capSolver{
		memo: make(map[memoKey]*ptree), seen: make(map[memoKey]bool),
		cmemo: make(map[concatKey][]*ptree), cseen: make(map[concatKey]bool),
		rmemo: make(map[repKey][]*ptree), rseen: make(map[repKey]bool),
		ctrA: make([]int, re.minSlots), ctrB: make([]int, re.minSlots),
	}
}

// solveCaptures fills caps (character spans) for the fixed span [so, eo).
func (re *Regexp) solveCaptures(d *decoded, so, eo int, eflags ExecFlags, caps []Match) *Error {
	s := re.getSolver()
	s.re, s.d, s.eflags = re, d, eflags
	defer func() {
		s.d = nil
		re.capPool.Put(s)
	}()
	best := s.bestParse(re.root, so, eo)
	if s.failed {
		return compileError(ESpace, -1)
	}
	if best == nil {
		// Phase A guarantees a parse exists; reaching this is a bug.
		return compileError(ESpace, -1)
	}
	for idx := range caps {
		caps[idx] = Match{-1, -1}
	}
	caps[0] = Match{so, eo}
	assignCaps(re, best, caps)
	return nil
}

func assignCaps(re *Regexp, t *ptree, caps []Match) {
	if t.n.op == opGroup {
		for _, inner := range re.nested[t.n.index] {
			caps[inner] = Match{-1, -1}
		}
		caps[t.n.index] = Match{t.i, t.j}
	}
	for _, kid := range t.kids {
		assignCaps(re, kid, caps)
	}
}
