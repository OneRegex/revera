package revera

// This package is the Vego rewrite of the reference engine in dev/internal/reference.
// It is a cleanroom implementation of the POSIX.1-2024 Issue 8 Extended Regular Expression language.
// docs/POSIX-1-2024-ERE-SPECIFICATION.md in this repository is the contract it follows.
//
// The API mirrors the regcomp() family.
// Patterns and subjects are UTF-8, length-delimited strings.
// NUL is an ordinary character.
// That input lies outside the C-representable domain, so it is a permitted extension.
// Dot never matches NUL, and bytes that are not valid UTF-8 match nothing.
//
// The subset has no methods, so every operation is a function that takes the Regexp as its first argument.
// Compile returns the Regexp by value, and nothing may change it afterwards.
// Exec never writes to it, so concurrent calls on one compiled value are safe in the Go instantiation.

// Regexp is a compiled regular expression.
type Regexp struct {
	// nodes is the AST arena, and root indexes it.
	nodes []node
	// brackets is the bracket arena, and opBracket nodes index it.
	brackets []bracketSet
	root     int32
	nsub     int
	flags    uint32
	loc      Locale
	// minSlots counts shortest-preferring repetitions.
	// Each one holds a counter slot, assigned in pattern pre-order.
	minSlots int
	// nested lists, for every group, the groups strictly inside it.
	nested [][]int32
	// multi is true when some bracket can consume several characters.
	multi bool
	// progOK is false when interval expansion passed the program size cap.
	// minLen then holds the smallest possible match length in characters, for the fallback in Exec.
	progOK bool
	prog   program
	minLen int
	// anchors is true when the pattern contains ^ or $ anywhere.
	anchors bool
	// onePass is true when every span has at most one parse.
	// The capture walk in onepass.go then replaces the phase B solver.
	onePass bool
}

// subjectLimit is the largest subject Exec accepts, in bytes.
// Thread payloads store match starts as int32, so a longer subject reports ESpace.
// The resource contract clamps its input bound to this value.
const subjectLimit = 1<<31 - 1

// lengthCap saturates minimum-length arithmetic well above any real subject size.
const lengthCap = int(1) << 40

// compileScan walks the pattern in pre-order and fills the tree-derived Regexp fields.
// Those fields are the minimal counter slots, the multi-character flag, and the anchor flag.
func compileScan(re *Regexp, ni int32) {
	switch re.nodes[ni].op {
	case opRepeat:
		if re.nodes[ni].minimal {
			re.nodes[ni].index = re.minSlots
			re.minSlots++
		}
	case opBracket:
		if bracketHasMultiMembers(re.brackets, re.nodes[ni].br) {
			re.multi = true
		}
	case opBOL, opEOL:
		re.anchors = true
	}
	for i := 0; i < len(re.nodes[ni].ch); i++ {
		compileScan(re, re.nodes[ni].ch[i])
	}
}

// groupStack tracks the open groups while collectNested walks the pattern.
type groupStack struct {
	g []int32
}

// collectNested records, for each group, the group numbers inside it.
func collectNested(re *Regexp, ni int32, stack *groupStack) {
	isGroup := re.nodes[ni].op == opGroup
	if isGroup {
		gi := int32(re.nodes[ni].index)
		for k := 0; k < len(stack.g); k++ {
			outer := stack.g[k]
			re.nested[outer] = append(re.nested[outer], gi)
		}
		stack.g = append(stack.g, gi)
	}
	for i := 0; i < len(re.nodes[ni].ch); i++ {
		collectNested(re, re.nodes[ni].ch[i], stack)
	}
	if isGroup {
		stack.g = stack.g[:len(stack.g)-1]
	}
}

// minMatchChars returns the smallest character count the node can consume.
// The result saturates at lengthCap.
func minMatchChars(nodes []node, brs []bracketSet, loc *Locale, ni int32) int {
	switch nodes[ni].op {
	case opChar, opAny:
		return 1
	case opBracket:
		return bracketMinChars(brs, nodes[ni].br, loc)
	case opBOL, opEOL:
		return 0
	case opGroup:
		return minMatchChars(nodes, brs, loc, nodes[ni].ch[0])
	case opConcat:
		total := 0
		for i := 0; i < len(nodes[ni].ch); i++ {
			total += minMatchChars(nodes, brs, loc, nodes[ni].ch[i])
			if total > lengthCap {
				return lengthCap
			}
		}
		return total
	case opAlt:
		best := lengthCap
		for i := 0; i < len(nodes[ni].ch); i++ {
			best = min(best, minMatchChars(nodes, brs, loc, nodes[ni].ch[i]))
		}
		return best
	case opRepeat:
		if nodes[ni].min == 0 {
			return 0
		}
		product := nodes[ni].min * minMatchChars(nodes, brs, loc, nodes[ni].ch[0])
		return min(product, lengthCap)
	}
	return 0
}

// Compile parses and compiles pattern in the given locale.
// LocalePOSIX() gives the POSIX locale.
// A nonzero code in the returned Error means failure, and the Regexp is then meaningless.
func Compile(pattern string, loc Locale, flags uint32) (Regexp, Error) {
	var re Regexp
	if !LocaleValid(&loc) {
		return re, compileError(ErrBadPat, -1)
	}
	var p parser
	root := parse(&p, &loc, pattern, flags)
	if root < 0 {
		return re, p.err
	}
	re.nsub = p.groups
	re.nodes = p.nodes
	re.brackets = p.brackets
	re.root = root
	re.flags = flags
	re.loc = loc
	re.nested = make([][]int32, re.nsub+1)
	compileScan(&re, root)
	var stack groupStack
	collectNested(&re, root, &stack)
	computeLengths(re.nodes, &re.loc, re.brackets, root)
	re.onePass = onePassAnalyze(re.nodes, re.brackets, root)
	var b progBuilder
	b.icase = flags&FlagICase != 0
	compileProgram(&b, re.nodes, root, re.multi, flags&FlagNewline != 0)
	if b.errCode != ErrNone {
		return re, compileError(b.errCode, -1)
	}
	if b.tooBig {
		re.minLen = minMatchChars(re.nodes, re.brackets, &re.loc, root)
	} else {
		re.progOK = true
		re.prog = b.prog
	}
	return re, noError()
}

// NumSub returns the number of parenthesized subexpressions, like re_nsub.
func NumSub(re *Regexp) int {
	return re.nsub
}

// trivialNullMatch reports whether existence alone answers an Exec call.
// A nullable, anchor-free pattern matches the null string at the start of any subject.
// The call also has to ask for no offsets.
func trivialNullMatch(re *Regexp, pmatch []Match) bool {
	return re.nodes[re.root].minL == 0 && !re.anchors &&
		(re.flags&FlagNoSub != 0 || len(pmatch) == 0)
}

// Exec searches subject for the POSIX-selected match.
//
// On success it returns true and fills pmatch like regexec().
// Element 0 is the whole match, element i is capturing subexpression i, and every remaining element becomes -1, -1.
// pmatch stays untouched when the expression was compiled with FlagNoSub, or when pmatch is empty.
// Exec returns false when no match exists, and the Error code is then ErrNone for a plain mismatch.
func Exec(re *Regexp, subject string, pmatch []Match, eflags uint32) (bool, Error) {
	if len(subject) > subjectLimit {
		return false, compileError(ErrESpace, -1)
	}
	if !re.progOK {
		// The expanded program passed the size cap.
		// A subject with fewer bytes than the minimum match length cannot match.
		// A longer one would really need the huge program.
		// The matcherContract fallback branch mirrors the cost of this path.
		if len(subject) < re.minLen {
			return false, noError()
		}
		if runeCount(subject) < re.minLen {
			// The byte count passed, but lengths compare in characters.
			return false, noError()
		}
		if trivialNullMatch(re, pmatch) {
			return true, noError()
		}
		return false, compileError(ErrESpace, -1)
	}
	if re.prog.failMin != failMinNone &&
		len(subject) >= re.prog.failMin &&
		runeCount(subject) >= re.prog.failMin {
		// An oversized subtree was pruned, and this subject is long enough to reach it.
		// The program may therefore miss matches or pick the wrong spans.
		// Pruning removes possibilities and adds none, so a match the program still finds proves existence.
		// A miss proves nothing, and offsets would need the full program.
		if trivialNullMatch(re, pmatch) {
			return true, noError()
		}
		if re.flags&FlagNoSub != 0 || len(pmatch) == 0 {
			probe := runPhaseA(re, subject, eflags)
			if probe.matched {
				return true, noError()
			}
		}
		return false, compileError(ErrESpace, -1)
	}
	result := runPhaseA(re, subject, eflags)
	if !result.matched {
		return false, noError()
	}
	if re.flags&FlagNoSub != 0 || len(pmatch) == 0 {
		return true, noError()
	}
	setMatch(pmatch, 0, result.so, result.eo)
	for idx := 1; idx < len(pmatch); idx++ {
		setMatch(pmatch, idx, -1, -1)
	}
	if len(pmatch) == 1 || re.nsub == 0 {
		return true, noError()
	}

	// Captures need the best parse of the selected span only.
	d := decodeWindow(subject, result.so, result.eo)
	caps := make([]Match, re.nsub+1)
	serr := solveCaptures(re, &d, 0, len(d.runes), eflags, caps)
	if serr.Code != ErrNone {
		return false, serr
	}
	fillMatches(re, &d, caps, pmatch)
	return true, noError()
}
