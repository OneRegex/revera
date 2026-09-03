package reference

// This file holds the one-pass capture path.
// Compile-time analysis can prove that every subject span has at most one parse of the pattern.
// The selection order then has nothing to choose.
// Phase B can therefore read the group spans from one deterministic walk, instead of running the memoized best-parse search.
// This meets the POSIX proof obligation for one-pass execution.
// Parse uniqueness leaves no two derivations whose ordering or captures could differ.
//
// The walk checks every step it takes.
// On any inconsistency it reports failure, and the caller returns ESpace.
// This keeps the compile-time backend choice and its resource contract intact.

import "slices"

// onePassNode reports whether every span has at most one parse of n.
// It also reports whether one deterministic walk can find that parse.
// It stores the branch first sets on alternation nodes that need lookahead.
func onePassNode(n *node) bool {
	switch n.op {
	case opChar, opAny, opBOL, opEOL:
		return true
	case opBracket:
		// A multi-character element makes the consumed length ambiguous.
		return !n.br.hasMultiMembers()
	case opGroup:
		return onePassNode(n.ch[0])
	case opConcat:
		variable := 0
		for _, child := range n.ch {
			if !onePassNode(child) {
				return false
			}
			if !fixedLength(child) {
				variable++
			}
		}
		// With at most one variable-length child, arithmetic on the span length gives every split point.
		return variable <= 1
	case opRepeat:
		if n.max == 0 {
			return true
		}
		// A fixed nonempty instance length forces the instance count.
		// It also rules out null occurrences.
		return fixedLength(n.ch[0]) && n.ch[0].minL > 0 &&
			onePassNode(n.ch[0])
	case opAlt:
		for _, branch := range n.ch {
			if !onePassNode(branch) {
				return false
			}
		}
		if disjointLengths(n.ch) {
			return true
		}
		return disjointFirsts(n)
	}
	return false
}

// fixedLength reports whether every match of n has the same length.
// Two saturated bounds compare equal without being exact, so they never count as fixed.
func fixedLength(n *node) bool {
	return n.minL == n.maxL && n.maxL < lenInf
}

// disjointLengths reports whether the branch length ranges never overlap.
// The span length alone then selects the branch.
func disjointLengths(branches []*node) bool {
	for i, a := range branches {
		for _, b := range branches[i+1:] {
			if a.minL <= b.maxL && b.minL <= a.maxL {
				return false
			}
		}
	}
	return true
}

// firstSet returns the exact set of characters that can begin a nonempty match of n.
// exact is false when the set is not enumerable, as for dot and bracket expressions.
func firstSet(n *node) (set []rune, exact bool) {
	switch n.op {
	case opChar:
		if n.fold != nil {
			return n.fold, true
		}
		return []rune{n.r}, true
	case opBOL, opEOL:
		return nil, true
	case opGroup:
		return firstSet(n.ch[0])
	case opRepeat:
		if n.max == 0 {
			return nil, true
		}
		return firstSet(n.ch[0])
	case opAlt:
		var out []rune
		for _, branch := range n.ch {
			sub, ok := firstSet(branch)
			if !ok {
				return nil, false
			}
			for _, r := range sub {
				out = appendUnique(out, r)
			}
		}
		return out, true
	case opConcat:
		var out []rune
		for _, child := range n.ch {
			sub, ok := firstSet(child)
			if !ok {
				return nil, false
			}
			for _, r := range sub {
				out = appendUnique(out, r)
			}
			if child.minL > 0 {
				break
			}
		}
		return out, true
	}
	return nil, false
}

// disjointFirsts checks that one lookahead character selects the branch.
// Every branch needs an exact first set, and the sets must never overlap.
// At most one branch may have a null match.
// On success it stores the sets for the walk.
func disjointFirsts(n *node) bool {
	firsts := make([][]rune, len(n.ch))
	nullable := 0
	for i, branch := range n.ch {
		set, ok := firstSet(branch)
		if !ok {
			return false
		}
		if branch.minL == 0 {
			nullable++
		}
		firsts[i] = set
	}
	if nullable > 1 {
		return false
	}
	for i, a := range firsts {
		for _, b := range firsts[i+1:] {
			for _, r := range a {
				if slices.Contains(b, r) {
					return false
				}
			}
		}
	}
	n.firsts = firsts
	return true
}

// onePassCaps fills the group spans from the unique parse of n over [i, j).
// It returns false when the walk hits an inconsistency.
func (re *Regexp) onePassCaps(d *decoded, n *node, i, j int, eflags ExecFlags, caps []Match) bool {
	return re.onePassCapsMeasured(d, n, i, j, eflags, caps, nil)
}

// onePassWork records recursive calls and loops written directly in onePassCapsMeasured.
// Helper work such as slices.Contains scans is covered by the contract but not this test meter.
type onePassWork struct {
	calls int64
	loops int64
}

func (w *onePassWork) call() {
	if w != nil {
		w.calls++
	}
}

func (w *onePassWork) loop() {
	if w != nil {
		w.loops++
	}
}

func (re *Regexp) onePassCapsMeasured(d *decoded, n *node, i, j int, eflags ExecFlags, caps []Match, work *onePassWork) bool {
	work.call()
	span := j - i
	if span < n.minL || span > n.maxL {
		return false
	}
	switch n.op {
	case opChar:
		return re.charMatches(n, d.runes[i])
	case opAny:
		return re.anyMatches(d.runes[i])
	case opBracket:
		return n.br.matchesOne(d.runes[i])
	case opBOL:
		return re.atBOL(d, i, eflags)
	case opEOL:
		return re.atEOL(d, i, eflags)
	case opGroup:
		// Entry into a group clears every group nested inside it.
		// This is the same section 12.7 rule that assignCaps applies.
		for _, inner := range re.nested[n.index] {
			work.loop()
			caps[inner] = Match{-1, -1}
		}
		caps[n.index] = Match{i, j}
		return re.onePassCapsMeasured(d, n.ch[0], i, j, eflags, caps, work)
	case opConcat:
		rest := span
		for _, child := range n.ch {
			work.loop()
			rest -= child.minL
		}
		at := i
		for _, child := range n.ch {
			work.loop()
			length := child.minL
			if !fixedLength(child) {
				// The single variable child takes the remainder.
				length += rest
			}
			if length < 0 || !re.onePassCapsMeasured(d, child, at, at+length, eflags, caps, work) {
				return false
			}
			at += length
		}
		return at == j
	case opRepeat:
		if span == 0 {
			return true
		}
		size := n.ch[0].minL
		count := span / size
		if span%size != 0 || count < n.min ||
			(n.max != infinite && count > n.max) {
			return false
		}
		at := i
		for range count {
			work.loop()
			if !re.onePassCapsMeasured(d, n.ch[0], at, at+size, eflags, caps, work) {
				return false
			}
			at += size
		}
		return true
	case opAlt:
		chosen := -1
		for idx, branch := range n.ch {
			work.loop()
			if span < branch.minL || span > branch.maxL {
				continue
			}
			if chosen >= 0 {
				chosen = -1
				break
			}
			chosen = idx
		}
		if chosen < 0 && n.firsts != nil {
			if span == 0 {
				for idx, branch := range n.ch {
					work.loop()
					if branch.minL == 0 {
						chosen = idx
						break
					}
				}
			} else {
				for idx := range n.ch {
					work.loop()
					if slices.Contains(n.firsts[idx], d.runes[i]) {
						chosen = idx
						break
					}
				}
			}
		}
		if chosen < 0 {
			return false
		}
		return re.onePassCapsMeasured(d, n.ch[chosen], i, j, eflags, caps, work)
	}
	return false
}
