// Package revera is a cleanroom Go implementation of the POSIX.1-2024 Issue 8 Extended Regular Expression language.
// docs/POSIX-1-2024-ERE-SPECIFICATION.md in this repository is the contract it follows.
//
// The API mirrors the regcomp() family in Go form.
// Patterns and subjects are UTF-8, length-delimited strings.
// NUL is an ordinary character.
// That input lies outside the C-representable domain, so it is a permitted extension.
// Dot never matches NUL, and bytes that are not valid UTF-8 match nothing.
package reference

import (
	"sync"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

// Match reports one substring as byte offsets, half-open [So, Eo).
// A nonparticipating subexpression has So == -1 and Eo == -1.
type Match struct {
	So int
	Eo int
}

// Regexp is a compiled regular expression.
// It never changes after compilation, and concurrent Exec calls are safe.
type Regexp struct {
	root  *node
	nsub  int
	flags CompileFlags
	loc   locale.Locale
	// minSlots counts shortest-preferring repetitions.
	// Each one holds a counter slot, assigned in pattern pre-order.
	minSlots int
	// nested lists, for every group, the groups strictly inside it.
	nested [][]int
	// multi is true when some bracket can consume several characters.
	multi bool
	prog  *program
	// onePass is true when every span has at most one parse.
	// The capture walk in onepass.go then replaces the phase B solver.
	onePass bool
	pool    sync.Pool
	capPool sync.Pool
}

// Compile parses and compiles pattern in the given locale.
// The POSIX locale is locale.POSIX().
func Compile(pattern string, loc locale.Locale, flags CompileFlags) (*Regexp, error) {
	if !loc.Valid() {
		return nil, compileError(BadPat, -1)
	}
	root, groups, perr := parse(pattern, loc, flags)
	if perr != nil {
		return nil, perr
	}
	re := &Regexp{root: root, nsub: groups, flags: flags, loc: loc}
	re.nested = make([][]int, groups+1)
	var groupStack []int
	walk(root, func(n *node) {
		switch n.op {
		case opRepeat:
			if n.minimal {
				n.index = re.minSlots
				re.minSlots++
			}
		case opBracket:
			if n.br.hasMultiMembers() {
				re.multi = true
			}
		}
	})
	collectNested(root, &groupStack, re.nested)
	computeLengths(root)
	re.onePass = onePassNode(root)
	prog, cerr := compileProgram(re)
	if cerr != nil {
		return nil, cerr
	}
	re.prog = prog
	return re, nil
}

// subjectLimit is the largest subject Exec accepts, in bytes.
// Thread payloads store match starts as int32, so a longer subject reports ESpace.
// The resource contract clamps its input bound to this value.
const subjectLimit = int64(1)<<31 - 1

// bracketMinChars returns the smallest character count one bracket match can consume.
// Only a positive list of nothing but multi-character collating symbols and equivalence classes can need more than one character.
// An equivalence class contributes the length of the shortest element in its class.
// ICase never changes match lengths.
func bracketMinChars(b *bracketSet) int64 {
	if b.negated || len(b.ranges) > 0 || b.classMask != 0 ||
		(len(b.elems) == 0 && len(b.equivs) == 0) {
		return 1
	}
	best := int64(lenInf)
	for _, e := range b.elems {
		best = min(best, int64(len(e)))
	}
	for _, eq := range b.equivs {
		best = min(best, int64(b.loc.MinEquivLength(eq)))
	}
	return best
}

// walk visits every node in pre-order.
func walk(n *node, visit func(*node)) {
	visit(n)
	for _, child := range n.ch {
		walk(child, visit)
	}
}

// collectNested records, for each group, the group numbers inside it.
func collectNested(n *node, stack *[]int, nested [][]int) {
	if n.op == opGroup {
		for _, outer := range *stack {
			nested[outer] = append(nested[outer], n.index)
		}
		*stack = append(*stack, n.index)
	}
	for _, child := range n.ch {
		collectNested(child, stack, nested)
	}
	if n.op == opGroup {
		*stack = (*stack)[:len(*stack)-1]
	}
}

// NumSub returns the number of parenthesized subexpressions, like re_nsub.
func (re *Regexp) NumSub() int {
	return re.nsub
}

// Exec searches subject for the POSIX-selected match.
//
// On success it returns true and fills pmatch like regexec().
// Element 0 is the whole match, element i is capturing subexpression i, and every remaining element becomes -1, -1.
// pmatch stays untouched when the expression was compiled with NoSub, or when pmatch is empty.
// Exec returns false when no match exists.
func (re *Regexp) Exec(subject string, pmatch []Match, eflags ExecFlags) (bool, error) {
	if int64(len(subject)) > subjectLimit {
		return false, compileError(ESpace, -1)
	}
	result := re.runPhaseA(subject, eflags)
	if !result.matched {
		return false, nil
	}
	if re.flags&NoSub != 0 || len(pmatch) == 0 {
		return true, nil
	}
	pmatch[0] = Match{result.so, result.eo}
	for idx := 1; idx < len(pmatch); idx++ {
		pmatch[idx] = Match{-1, -1}
	}
	if len(pmatch) == 1 || re.nsub == 0 {
		return true, nil
	}

	// Captures need the best parse of the selected span only.
	d := decodeWindow(subject, result.so, result.eo)
	caps := make([]Match, re.nsub+1)
	if serr := re.solveCaptures(&d, 0, len(d.runes), eflags, caps); serr != nil {
		return false, serr
	}
	re.fillMatches(&d, caps, pmatch)
	return true, nil
}
