package revera

import (
	"slices"
	"strings"
	"unicode/utf8"

	"revera/locale"
)

type opKind uint8

const (
	opChar opKind = iota
	opAny
	opBracket
	opBOL
	opEOL
	opConcat
	opAlt
	opRepeat
	opGroup
)

// infinite marks an unbounded repetition maximum.
const infinite = -1

type node struct {
	op      opKind
	ch      []*node
	r       rune   // opChar: the literal character
	fold    []rune // opChar with ICase: the accepted closure set
	br      *bracketSet
	min     int  // opRepeat
	max     int  // opRepeat, where infinite means no bound
	minimal bool // opRepeat: shortest-preferring
	index   int  // opGroup capture number, or opRepeat minimal counter slot

	// minL and maxL bound the character count one match of this node can consume.
	// maxL saturates at lenInf.
	// The capture solver uses both to clamp its split ranges.
	minL int
	maxL int
	// sufMin and sufMax bound the children from index k onward, for opConcat only.
	sufMin []int
	sufMax []int
	// firsts holds the branch first sets of a one-pass alternation that needs lookahead to select its branch.
	firsts [][]rune
}

// lenInf saturates node length bounds far above any supported span.
const lenInf = 1 << 30

// computeLengths fills minL, maxL, and the concatenation suffix bounds, from the bottom up.
func computeLengths(n *node) {
	for _, child := range n.ch {
		computeLengths(child)
	}
	switch n.op {
	case opChar, opAny:
		n.minL, n.maxL = 1, 1
	case opBracket:
		n.minL = int(bracketMinChars(n.br))
		n.maxL = 1
		if n.br.hasMultiMembers() {
			n.maxL = locale.MaxElementLength()
		}
	case opBOL, opEOL:
		n.minL, n.maxL = 0, 0
	case opGroup:
		n.minL, n.maxL = n.ch[0].minL, n.ch[0].maxL
	case opConcat:
		count := len(n.ch)
		n.sufMin = make([]int, count+1)
		n.sufMax = make([]int, count+1)
		for k := count - 1; k >= 0; k-- {
			n.sufMin[k] = satAdd(n.sufMin[k+1], n.ch[k].minL)
			n.sufMax[k] = satAdd(n.sufMax[k+1], n.ch[k].maxL)
		}
		n.minL, n.maxL = n.sufMin[0], n.sufMax[0]
	case opAlt:
		n.minL, n.maxL = lenInf, 0
		for _, child := range n.ch {
			n.minL = min(n.minL, child.minL)
			n.maxL = max(n.maxL, child.maxL)
		}
	case opRepeat:
		n.minL = satMul(n.min, n.ch[0].minL)
		switch {
		case n.max == 0 || n.ch[0].maxL == 0:
			n.maxL = 0
		case n.max == infinite:
			n.maxL = lenInf
		default:
			n.maxL = satMul(n.max, n.ch[0].maxL)
		}
	}
}

func satAdd(a, b int) int {
	// int64 arithmetic avoids overflow on 32-bit platforms, where two saturated values pass the int range.
	return int(min(int64(a)+int64(b), int64(lenInf)))
}

func satMul(a, b int) int {
	product := int64(a) * int64(b)
	if product > lenInf {
		return lenInf
	}
	return int(product)
}

type parser struct {
	src   string
	pos   int
	flags CompileFlags
	loc   locale.Locale
	// groups counts opening parentheses in pattern order.
	groups int
}

func (p *parser) eof() bool {
	return p.pos >= len(p.src)
}

// peekByte returns the next byte without consuming it, or 0 at the end.
func (p *parser) peekByte() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) peekByteAt(ahead int) byte {
	if p.pos+ahead >= len(p.src) {
		return 0
	}
	return p.src[p.pos+ahead]
}

// nextRune consumes and returns one character.
// It reports invalid UTF-8.
func (p *parser) nextRune() (rune, *Error) {
	r, size := utf8.DecodeRuneInString(p.src[p.pos:])
	if r == utf8.RuneError && size <= 1 {
		return 0, compileError(BadPat, p.pos)
	}
	p.pos += size
	return r, nil
}

func parse(pattern string, loc locale.Locale, flags CompileFlags) (*node, int, *Error) {
	p := &parser{src: pattern, flags: flags, loc: loc}
	root, err := p.parseAlt(false)
	if err != nil {
		return nil, 0, err
	}
	if !p.eof() {
		// Only an unmatched ')' inside parseAlt(false) can stop early.
		// parseBranch treats that character as ordinary, so this case cannot happen.
		return nil, 0, compileError(BadPat, p.pos)
	}
	return root, p.groups, nil
}

// parseAlt parses branch { "|" branch }.
func (p *parser) parseAlt(inGroup bool) (*node, *Error) {
	first, err := p.parseBranch(inGroup)
	if err != nil {
		return nil, err
	}
	if p.peekByte() != '|' {
		return first, nil
	}
	alt := &node{op: opAlt, ch: []*node{first}}
	for p.peekByte() == '|' {
		p.pos++
		branch, err := p.parseBranch(inGroup)
		if err != nil {
			return nil, err
		}
		alt.ch = append(alt.ch, branch)
	}
	return alt, nil
}

// parseBranch parses a nonempty concatenation of expressions.
func (p *parser) parseBranch(inGroup bool) (*node, *Error) {
	var exprs []*node
	for {
		if p.eof() {
			break
		}
		c := p.peekByte()
		if c == '|' {
			break
		}
		if c == ')' && inGroup {
			break
		}
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, expr)
	}
	if len(exprs) == 0 {
		// An empty pattern, group, or branch is outside the grammar.
		return nil, compileError(BadPat, p.pos)
	}
	if len(exprs) == 1 {
		return exprs[0], nil
	}
	return &node{op: opConcat, ch: exprs}, nil
}

func isDupByte(c byte) bool {
	return c == '*' || c == '+' || c == '?' || c == '{'
}

// parseExpr parses one anchor, or one primary with an optional duplication.
func (p *parser) parseExpr() (*node, *Error) {
	start := p.pos
	c := p.peekByte()
	switch c {
	case '^', '$':
		p.pos++
		if isDupByte(p.peekByte()) {
			// A duplication cannot follow an anchor.
			return nil, compileError(BadRpt, p.pos)
		}
		if c == '^' {
			return &node{op: opBOL}, nil
		}
		return &node{op: opEOL}, nil
	case '*', '+', '?', '{':
		// The postfix operator lacks a permitted operand.
		return nil, compileError(BadRpt, start)
	}

	var primary *node
	var err *Error
	switch c {
	case '(':
		p.pos++
		p.groups++
		index := p.groups
		sub, err := p.parseAlt(true)
		if err != nil {
			return nil, err
		}
		if p.peekByte() != ')' {
			return nil, compileError(EParen, start)
		}
		p.pos++
		primary = &node{op: opGroup, ch: []*node{sub}, index: index}
	case '[':
		primary, err = p.parseBracket()
		if err != nil {
			return nil, err
		}
	case '\\':
		p.pos++
		if p.eof() {
			return nil, compileError(EEscape, start)
		}
		r, rerr := p.nextRune()
		if rerr != nil {
			return nil, rerr
		}
		switch r {
		case '^', '.', '[', ']', '$', '(', ')', '|', '*', '+', '?', '{', '}', '\\':
			primary = p.charNode(r)
		default:
			// A backslash before an ordinary character is undefined.
			return nil, compileError(BadPat, start)
		}
	case '.':
		p.pos++
		primary = &node{op: opAny}
	default:
		// ')' arrives here only at depth zero, where it is ordinary.
		r, rerr := p.nextRune()
		if rerr != nil {
			return nil, rerr
		}
		primary = p.charNode(r)
	}
	return p.parseDup(primary)
}

// charNode builds a literal atom and its ICase closure set.
func (p *parser) charNode(r rune) *node {
	n := &node{op: opChar, r: r}
	if p.flags&ICase != 0 {
		n.fold = appendUnique(n.fold, r)
		n.fold = appendUnique(n.fold, p.loc.ToUpper(r))
		n.fold = appendUnique(n.fold, p.loc.ToLower(r))
	}
	return n
}

func appendUnique(runes []rune, r rune) []rune {
	if slices.Contains(runes, r) {
		return runes
	}
	return append(runes, r)
}

// parseDup parses an optional duplication and its repetition modifier.
func (p *parser) parseDup(operand *node) (*node, *Error) {
	c := p.peekByte()
	var min, max int
	switch c {
	case '*':
		p.pos++
		min, max = 0, infinite
	case '+':
		p.pos++
		min, max = 1, infinite
	case '?':
		p.pos++
		min, max = 0, 1
	case '{':
		var err *Error
		min, max, err = p.parseInterval()
		if err != nil {
			return nil, err
		}
	default:
		return operand, nil
	}
	minimal := p.flags&Minimal != 0
	if p.peekByte() == '?' {
		p.pos++
		minimal = !minimal
	}
	if isDupByte(p.peekByte()) {
		// Adjacent duplication symbols past one modifier are undefined.
		return nil, compileError(BadRpt, p.pos)
	}
	return &node{op: opRepeat, ch: []*node{operand},
		min: min, max: max, minimal: minimal}, nil
}

// parseInterval parses "{m}", "{m,}", or "{m,n}" starting at '{'.
func (p *parser) parseInterval() (int, int, *Error) {
	start := p.pos
	p.pos++
	min, ok := p.parseCount()
	if !ok {
		return 0, 0, p.intervalError(start)
	}
	switch p.peekByte() {
	case '}':
		p.pos++
		return min, min, nil
	case ',':
		p.pos++
	default:
		return 0, 0, p.intervalError(start)
	}
	if p.peekByte() == '}' {
		p.pos++
		return min, infinite, nil
	}
	max, ok := p.parseCount()
	if !ok || p.peekByte() != '}' {
		return 0, 0, p.intervalError(start)
	}
	p.pos++
	if max < min {
		return 0, 0, compileError(BadBR, start)
	}
	return min, max, nil
}

// intervalError picks EBrace when the brace never closes, else BadBR.
func (p *parser) intervalError(start int) *Error {
	if strings.IndexByte(p.src[p.pos:], '}') >= 0 {
		return compileError(BadBR, start)
	}
	return compileError(EBrace, start)
}

// parseCount parses a decimal count from zero through DupMax.
func (p *parser) parseCount() (int, bool) {
	if p.eof() || p.src[p.pos] < '0' || p.src[p.pos] > '9' {
		return 0, false
	}
	value := 0
	for !p.eof() && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		value = value*10 + int(p.src[p.pos]-'0')
		if value > DupMax {
			return 0, false
		}
		p.pos++
	}
	return value, true
}
