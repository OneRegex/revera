package revera

// This file holds the reference matcher. It enumerates every parse of the
// pattern and applies the selection order of section 4.3 directly, so it
// is correct by construction. It is exponential in the worst case and
// exists as the semantic baseline; the fast engine must agree with it.

import "slices"

// decoded is a subject window decoded into characters with byte
// boundaries. The edge flags carry the anchor context of the text
// around the window.
type decoded struct {
	runes []rune
	// byteAt[i] is the absolute byte offset of character boundary i.
	// Its length is len(runes)+1.
	byteAt []int
	// atSubjectStart is true when the window begins the subject.
	atSubjectStart bool
	// atSubjectEnd is true when the window ends the subject.
	atSubjectEnd bool
	// prevIsNewline is true when a newline precedes the window.
	prevIsNewline bool
	// nextIsNewline is true when a newline follows the window.
	nextIsNewline bool
}

// invalidRune marks a byte that is not valid UTF-8. It matches nothing.
const invalidRune rune = -1

func decodeSubject(s string) decoded {
	return decodeWindow(s, 0, len(s))
}

// decodeWindow decodes s[so:eo]. Both offsets sit on character
// boundaries.
func decodeWindow(s string, so, eo int) decoded {
	d := decoded{
		runes:          make([]rune, 0, eo-so),
		byteAt:         make([]int, 0, eo-so+1),
		atSubjectStart: so == 0,
		atSubjectEnd:   eo == len(s),
		prevIsNewline:  so > 0 && s[so-1] == '\n',
		nextIsNewline:  eo < len(s) && s[eo] == '\n',
	}
	for i := so; i < eo; {
		d.byteAt = append(d.byteAt, i)
		r, size := decodeRune(s[i:])
		d.runes = append(d.runes, r)
		i += size
	}
	d.byteAt = append(d.byteAt, eo)
	return d
}

// decodeRune decodes one UTF-8 character, mapping encoding errors to the
// invalid-byte sentinel one byte at a time.
func decodeRune(s string) (rune, int) {
	c := s[0]
	if c < 0x80 {
		return rune(c), 1
	}
	var size int
	var r rune
	switch {
	case c&0xe0 == 0xc0:
		size, r = 2, rune(c&0x1f)
	case c&0xf0 == 0xe0:
		size, r = 3, rune(c&0x0f)
	case c&0xf8 == 0xf0:
		size, r = 4, rune(c&0x07)
	default:
		return invalidRune, 1
	}
	if len(s) < size {
		return invalidRune, 1
	}
	for i := 1; i < size; i++ {
		if s[i]&0xc0 != 0x80 {
			return invalidRune, 1
		}
		r = r<<6 | rune(s[i]&0x3f)
	}
	if r > 0x10ffff || (r >= 0xd800 && r <= 0xdfff) {
		return invalidRune, 1
	}
	// Reject overlong encodings; they would alias shorter characters.
	minValue := [5]rune{0, 0, 0x80, 0x800, 0x10000}[size]
	if r < minValue {
		return invalidRune, 1
	}
	return r, size
}

// atom matching helpers, shared with the engine.

func (re *Regexp) charMatches(n *node, c rune) bool {
	if c < 0 {
		return false
	}
	if re.flags&ICase != 0 {
		return slices.Contains(n.fold, c)
	}
	return c == n.r
}

func (re *Regexp) anyMatches(c rune) bool {
	if c <= 0 {
		// Dot never matches NUL, and never an invalid byte.
		return false
	}
	if re.flags&Newline != 0 && c == '\n' {
		return false
	}
	return true
}

func (re *Regexp) atBOL(d *decoded, i int, eflags ExecFlags) bool {
	if i == 0 {
		if d.atSubjectStart {
			return eflags&NotBOL == 0
		}
		return re.flags&Newline != 0 && d.prevIsNewline
	}
	return re.flags&Newline != 0 && d.runes[i-1] == '\n'
}

func (re *Regexp) atEOL(d *decoded, i int, eflags ExecFlags) bool {
	if i == len(d.runes) {
		if d.atSubjectEnd {
			return eflags&NotEOL == 0
		}
		return re.flags&Newline != 0 && d.nextIsNewline
	}
	return re.flags&Newline != 0 && d.runes[i] == '\n'
}

// ptree is one parse of a pattern node over a character span.
type ptree struct {
	n      *node
	i, j   int
	branch int // opAlt: index of the selected branch
	kids   []*ptree
}

type memoKey struct {
	n    *node
	i, j int
}

type oracle struct {
	re     *Regexp
	d      *decoded
	eflags ExecFlags
	memo   map[memoKey][]*ptree
	work   int
	failed bool
}

// oracleWorkLimit caps enumeration so a pathological test fails loudly
// instead of hanging.
const oracleWorkLimit = 50_000_000

func (o *oracle) step() bool {
	o.work++
	if o.work > oracleWorkLimit {
		o.failed = true
	}
	return !o.failed
}

// parses returns every parse of n over exactly [i, j).
func (o *oracle) parses(n *node, i, j int) []*ptree {
	key := memoKey{n, i, j}
	if cached, ok := o.memo[key]; ok {
		return cached
	}
	if !o.step() {
		return nil
	}
	var out []*ptree
	switch n.op {
	case opChar:
		if j == i+1 && o.re.charMatches(n, o.d.runes[i]) {
			out = append(out, &ptree{n: n, i: i, j: j})
		}
	case opAny:
		if j == i+1 && o.re.anyMatches(o.d.runes[i]) {
			out = append(out, &ptree{n: n, i: i, j: j})
		}
	case opBracket:
		if o.bracketSpanOK(n, i, j) {
			out = append(out, &ptree{n: n, i: i, j: j})
		}
	case opBOL:
		if j == i && o.re.atBOL(o.d, i, o.eflags) {
			out = append(out, &ptree{n: n, i: i, j: j})
		}
	case opEOL:
		if j == i && o.re.atEOL(o.d, i, o.eflags) {
			out = append(out, &ptree{n: n, i: i, j: j})
		}
	case opGroup:
		for _, sub := range o.parses(n.ch[0], i, j) {
			out = append(out, &ptree{n: n, i: i, j: j, kids: []*ptree{sub}})
		}
	case opAlt:
		for b, branch := range n.ch {
			for _, sub := range o.parses(branch, i, j) {
				out = append(out, &ptree{n: n, i: i, j: j, branch: b,
					kids: []*ptree{sub}})
			}
		}
	case opConcat:
		for _, kids := range o.concatParses(n.ch, i, j) {
			out = append(out, &ptree{n: n, i: i, j: j, kids: kids})
		}
	case opRepeat:
		if i == j && n.min == 0 {
			// A null repetition match takes one null occurrence when
			// its operand has one: a null match is its only available
			// match, and null beats nonparticipation (sections 8.5 and
			// 4.3). With no null operand match, or a zero maximum, the
			// repetition selects zero occurrences.
			var subs []*ptree
			if n.max != 0 {
				subs = o.parses(n.ch[0], i, i)
			}
			if len(subs) == 0 {
				out = append(out, &ptree{n: n, i: i, j: j})
			}
			for _, sub := range subs {
				out = append(out, &ptree{n: n, i: i, j: j,
					kids: []*ptree{sub}})
			}
			break
		}
		for _, kids := range o.repeatParses(n, i, j, 0, false) {
			out = append(out, &ptree{n: n, i: i, j: j, kids: kids})
		}
	}
	o.memo[key] = out
	return out
}

func (o *oracle) bracketSpanOK(n *node, i, j int) bool {
	k := j - i
	if k < 1 {
		return false
	}
	newlineMode := o.re.flags&Newline != 0
	icase := o.re.flags&ICase != 0
	if k == 1 {
		return n.br.matchesOne(o.d.runes[i], o.re.loc, icase, newlineMode)
	}
	if !n.br.hasMultiMembers() {
		return false
	}
	return n.br.matchesMulti(o.d.runes[i:j], o.re.loc, icase)
}

// concatParses enumerates child parse lists covering [i, j) in order.
func (o *oracle) concatParses(children []*node, i, j int) [][]*ptree {
	if !o.step() {
		return nil
	}
	if len(children) == 1 {
		var out [][]*ptree
		for _, sub := range o.parses(children[0], i, j) {
			out = append(out, []*ptree{sub})
		}
		return out
	}
	var out [][]*ptree
	for m := i; m <= j; m++ {
		heads := o.parses(children[0], i, m)
		if len(heads) == 0 {
			continue
		}
		tails := o.concatParses(children[1:], m, j)
		for _, head := range heads {
			for _, tail := range tails {
				kids := make([]*ptree, 0, len(tail)+1)
				kids = append(kids, head)
				kids = append(kids, tail...)
				out = append(out, kids)
			}
		}
	}
	return out
}

// repeatParses enumerates instance lists for a repetition over [i, j).
// done counts instances already taken. hasEmpty tracks whether some
// instance was a null match. The empty-occurrence rule of section 8.5
// allows a null instance only when the final count stays at the minimum.
func (o *oracle) repeatParses(n *node, i, j, done int, hasEmpty bool) [][]*ptree {
	if !o.step() {
		return nil
	}
	var out [][]*ptree
	if i == j && done >= n.min && (!hasEmpty || done == n.min) {
		out = append(out, nil)
	}
	if n.max != infinite && done == n.max {
		return out
	}
	if hasEmpty && done >= n.min {
		return out
	}
	for m := i; m <= j; m++ {
		if m == i && done >= n.min {
			// A null occurrence past the minimum is never taken.
			continue
		}
		heads := o.parses(n.ch[0], i, m)
		if len(heads) == 0 {
			continue
		}
		tails := o.repeatParses(n, m, j, done+1, hasEmpty || m == i)
		for _, head := range heads {
			for _, tail := range tails {
				kids := make([]*ptree, 0, len(tail)+1)
				kids = append(kids, head)
				kids = append(kids, tail...)
				out = append(out, kids)
			}
		}
	}
	return out
}

// addCounters accumulates the consumed totals of every shortest-preferring
// repetition, by counter slot.
func addCounters(t *ptree, out []int) {
	if t.n.op == opRepeat && t.n.minimal {
		out[t.n.index] += t.j - t.i
	}
	for _, kid := range t.kids {
		addCounters(kid, out)
	}
}

// structCmp compares two parses of the same pattern node in pre-order.
// It returns a negative value when a wins, positive when b wins.
func structCmp(a, b *ptree) int {
	spanA, spanB := a.j-a.i, b.j-b.i
	if spanA != spanB {
		if a.n.op == opRepeat && a.n.minimal {
			return spanA - spanB
		}
		return spanB - spanA
	}
	switch a.n.op {
	case opAlt:
		if a.branch != b.branch {
			// Equal outer results: the earlier branch participates at
			// an earlier pre-order position, so it wins.
			return a.branch - b.branch
		}
		return structCmp(a.kids[0], b.kids[0])
	case opConcat, opGroup:
		for idx := range a.kids {
			if c := structCmp(a.kids[idx], b.kids[idx]); c != 0 {
				return c
			}
		}
	case opRepeat:
		limit := min(len(a.kids), len(b.kids))
		for idx := range limit {
			ka, kb := a.kids[idx], b.kids[idx]
			sa, sb := ka.j-ka.i, kb.j-kb.i
			if sa != sb {
				if a.n.minimal {
					return sa - sb
				}
				return sb - sa
			}
		}
		if len(a.kids) != len(b.kids) {
			return len(a.kids) - len(b.kids)
		}
		for idx := range a.kids {
			if c := structCmp(a.kids[idx], b.kids[idx]); c != 0 {
				return c
			}
		}
	}
	return 0
}

// betterCandidate applies the full selection order at one start position:
// minimal counters, then whole length, then structure.
func (o *oracle) betterCandidate(a, b *ptree, ca, cb []int) bool {
	for idx := range ca {
		if ca[idx] != cb[idx] {
			return ca[idx] < cb[idx]
		}
	}
	if a.j != b.j {
		return a.j > b.j
	}
	return structCmp(a, b) < 0
}

// assignCaptures records group spans from the winning parse. Entering a
// group clears every group nested inside it, which implements the
// recursive last-participation rule of section 12.7.
func (o *oracle) assignCaptures(t *ptree, caps []Match) {
	if t.n.op == opGroup {
		for _, inner := range o.re.nested[t.n.index] {
			caps[inner] = Match{-1, -1}
		}
		caps[t.n.index] = Match{t.i, t.j}
	}
	for _, kid := range t.kids {
		o.assignCaptures(kid, caps)
	}
}

// oracleExec runs the reference matcher. Capture offsets come back in
// character positions; the caller converts them to bytes.
func (re *Regexp) oracleExec(d *decoded, eflags ExecFlags) (bool, []Match, error) {
	o := &oracle{re: re, d: d, eflags: eflags, memo: make(map[memoKey][]*ptree)}
	n := len(d.runes)
	for start := 0; start <= n; start++ {
		var best *ptree
		bestCounters := make([]int, re.minSlots)
		counters := make([]int, re.minSlots)
		for end := start; end <= n; end++ {
			for _, tree := range o.parses(re.root, start, end) {
				for idx := range counters {
					counters[idx] = 0
				}
				addCounters(tree, counters)
				if best == nil || o.betterCandidate(tree, best, counters, bestCounters) {
					best = tree
					copy(bestCounters, counters)
				}
			}
		}
		if o.failed {
			return false, nil, compileError(ESpace, -1)
		}
		if best != nil {
			caps := make([]Match, re.nsub+1)
			for idx := range caps {
				caps[idx] = Match{-1, -1}
			}
			caps[0] = Match{best.i, best.j}
			o.assignCaptures(best, caps)
			return true, caps, nil
		}
	}
	return false, nil, nil
}

// oracleFullExec runs the reference matcher end to end, including the
// pmatch conversion. The differential tests compare it with exec.
func (re *Regexp) oracleFullExec(subject string, pmatch []Match, eflags ExecFlags) (bool, error) {
	d := decodeSubject(subject)
	ok, caps, err := re.oracleExec(&d, eflags)
	if err != nil || !ok {
		return false, err
	}
	re.fillMatches(&d, caps, pmatch)
	return true, nil
}

// fillMatches converts character spans to byte offsets and fills pmatch
// per section 12.5.
func (re *Regexp) fillMatches(d *decoded, caps []Match, pmatch []Match) {
	if re.flags&NoSub != 0 || len(pmatch) == 0 {
		return
	}
	for idx := range pmatch {
		if idx < len(caps) && caps[idx].So >= 0 {
			pmatch[idx] = Match{d.byteAt[caps[idx].So], d.byteAt[caps[idx].Eo]}
		} else {
			pmatch[idx] = Match{-1, -1}
		}
	}
}
