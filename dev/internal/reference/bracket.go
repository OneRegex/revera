package reference

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

type runeRange struct{ lo, hi rune }

// maxPreimages sizes the stack buffers for case preimages.
// It is only a hint, because a character with more preimages spills to the heap through append.
const maxPreimages = 4

// bracketSet is the compiled form of one bracket expression.
// The locale and the flags that shape matching are fixed at compile time.
// Every matcher therefore tests membership in the same context.
type bracketSet struct {
	negated   bool
	loc       locale.Locale
	icase     bool
	nlMode    bool
	ranges    []runeRange // sorted, non-overlapping single-character members
	classMask uint16      // union of standard LC_CTYPE classes
	elems     [][]rune    // explicit multi-character collating symbols
	equivs    [][]rune    // named equivalence-class elements
	// multiLens has bit L set when a multi-character match of length L is possible.
	// The executor therefore probes only those lengths.
	multiLens uint16
}

type bracketItemKind uint8

const (
	itemChar bracketItemKind = iota
	itemElem
	itemEquiv
	itemClass
)

type bracketItem struct {
	kind  bracketItemKind
	r     rune
	seq   []rune
	class locale.Class
}

// parseBracket parses a complete bracket expression starting at '['.
func (p *parser) parseBracket() (*node, *Error) {
	start := p.pos
	p.pos++
	b := &bracketSet{
		loc:    p.loc,
		icase:  p.flags&ICase != 0,
		nlMode: p.flags&Newline != 0,
	}
	if p.peekByte() == '^' {
		b.negated = true
		p.pos++
	}
	empty := true
	for {
		if p.eof() {
			return nil, compileError(EBrack, start)
		}
		if p.peekByte() == ']' && !empty {
			p.pos++
			b.finalize()
			return &node{op: opBracket, br: b}, nil
		}
		item, err := p.parseBracketItem(start)
		if err != nil {
			return nil, err
		}
		empty = false
		if item.kind == itemChar && p.peekByte() == '-' &&
			p.peekByteAt(1) != ']' && p.pos+1 < len(p.src) {
			rangeStart := p.pos
			p.pos++
			end, err := p.parseBracketItem(start)
			if err != nil {
				return nil, err
			}
			if end.kind != itemChar {
				return nil, compileError(ERange, rangeStart)
			}
			if !p.loc.SupportsRanges() {
				// Ranges in a non-POSIX locale use the permitted reject policy.
				return nil, compileError(ERange, rangeStart)
			}
			if item.r > end.r {
				// An empty range set uses the permitted invalid outcome.
				return nil, compileError(ERange, rangeStart)
			}
			b.ranges = append(b.ranges, runeRange{item.r, end.r})
			if p.peekByte() == '-' && p.peekByteAt(1) != ']' {
				// A shared range endpoint, as in [a-m-o], is undefined.
				return nil, compileError(ERange, p.pos)
			}
			continue
		}
		if item.kind != itemChar && p.peekByte() == '-' &&
			p.peekByteAt(1) != ']' && p.pos+1 < len(p.src) {
			// A class or equivalence class cannot start a range.
			return nil, compileError(ERange, p.pos)
		}
		switch item.kind {
		case itemChar:
			b.ranges = append(b.ranges, runeRange{item.r, item.r})
		case itemElem:
			b.elems = append(b.elems, item.seq)
		case itemEquiv:
			b.equivs = append(b.equivs, item.seq)
		case itemClass:
			b.classMask |= 1 << item.class
		}
	}
}

// parseBracketItem parses one list member.
// A member is an ordinary character, a literal leading ']', a collating symbol, an equivalence class, or a character class.
// A single-character collating symbol becomes an ordinary character item, so it stays usable as a range endpoint.
func (p *parser) parseBracketItem(bracketStart int) (bracketItem, *Error) {
	c := p.peekByte()
	if c == '[' {
		switch p.peekByteAt(1) {
		case '.':
			seq, err := p.scanInner(".]", ECollate)
			if err != nil {
				return bracketItem{}, err
			}
			if !p.loc.IsCollatingElement(seq) {
				// This case is invalid, not undefined.
				// The RE must fail when the element does not exist.
				return bracketItem{}, compileError(ECollate, bracketStart)
			}
			if len(seq) == 1 {
				return bracketItem{kind: itemChar, r: seq[0]}, nil
			}
			return bracketItem{kind: itemElem, seq: seq}, nil
		case '=':
			seq, err := p.scanInner("=]", ECollate)
			if err != nil {
				return bracketItem{}, err
			}
			if !p.loc.IsCollatingElement(seq) {
				return bracketItem{}, compileError(ECollate, bracketStart)
			}
			return bracketItem{kind: itemEquiv, seq: seq}, nil
		case ':':
			seq, err := p.scanInner(":]", ECType)
			if err != nil {
				return bracketItem{}, err
			}
			class, ok := locale.ClassByName(string(seq))
			if !ok {
				return bracketItem{}, compileError(ECType, bracketStart)
			}
			return bracketItem{kind: itemClass, class: class}, nil
		}
	}
	r, err := p.nextRune()
	if err != nil {
		return bracketItem{}, err
	}
	return bracketItem{kind: itemChar, r: r}, nil
}

// scanInner consumes "[X content X]", where X is '.', '=', or ':', and returns the content characters.
// emptyCode reports empty content.
func (p *parser) scanInner(closer string, emptyCode Code) ([]rune, *Error) {
	start := p.pos
	p.pos += 2
	rest := p.src[p.pos:]
	end := strings.Index(rest, closer)
	if end < 0 {
		return nil, compileError(EBrack, start)
	}
	if end == 0 {
		return nil, compileError(emptyCode, start)
	}
	content := rest[:end]
	p.pos += end + len(closer)
	if !utf8.ValidString(content) {
		return nil, compileError(BadPat, start)
	}
	return []rune(content), nil
}

// finalize sorts and merges the single-character ranges.
// It also records the lengths a multi-character match can take.
func (b *bracketSet) finalize() {
	if b.hasMultiMembers() {
		for _, e := range b.elems {
			b.multiLens |= 1 << len(e)
		}
		if len(b.equivs) > 0 {
			// An equivalence class can match any collating element with an equal primary weight, whatever its length.
			for length := 2; length <= locale.MaxElementLength(); length++ {
				b.multiLens |= 1 << length
			}
		}
	}
	if len(b.ranges) < 2 {
		return
	}
	slices.SortFunc(b.ranges, func(x, y runeRange) int {
		return cmp.Or(cmp.Compare(x.lo, y.lo), cmp.Compare(x.hi, y.hi))
	})
	merged := b.ranges[:1]
	for _, r := range b.ranges[1:] {
		last := &merged[len(merged)-1]
		if r.lo <= last.hi+1 {
			if r.hi > last.hi {
				last.hi = r.hi
			}
			continue
		}
		merged = append(merged, r)
	}
	b.ranges = merged
}

func (b *bracketSet) inRanges(c rune) bool {
	_, ok := slices.BinarySearchFunc(b.ranges, c, func(r runeRange, c rune) int {
		switch {
		case r.hi < c:
			return -1
		case r.lo > c:
			return 1
		}
		return 0
	})
	return ok
}

// positiveSingle tests case-sensitive membership of one character in the positive list.
// It ignores multi-character elements.
func (b *bracketSet) positiveSingle(c rune) bool {
	if b.inRanges(c) {
		return true
	}
	if b.classMask != 0 && b.loc.ClassMask(c)&b.classMask != 0 {
		return true
	}
	for _, eq := range b.equivs {
		single := [1]rune{c}
		if b.loc.PrimaryEqual(single[:], eq) {
			return true
		}
	}
	return false
}

// matchesOne tests whether the bracket accepts the single character c.
// Under ICase, the closure applies after inversion for a negated list, exactly as section 10.2 requires.
// The character matches when some case variant of it lands on the accepted side of the positive list.
func (b *bracketSet) matchesOne(c rune) bool {
	if c < 0 {
		// The invalid-byte sentinel matches nothing, not even a negated list.
		return false
	}
	if b.negated && b.nlMode && c == '\n' {
		return false
	}
	want := !b.negated
	if b.positiveSingle(c) == want {
		return true
	}
	if !b.icase {
		return false
	}
	var buffer [maxPreimages]rune
	for _, m := range b.loc.AppendCasePreimages(buffer[:0], c) {
		if b.positiveSingle(m) == want {
			return true
		}
	}
	return false
}

// counterpartMatch tests one subject character against one element character, under the ICase replacement rule.
func (b *bracketSet) counterpartMatch(t, e rune) bool {
	if t == e {
		return true
	}
	if !b.icase {
		return false
	}
	return t == b.loc.ToUpper(e) || t == b.loc.ToLower(e)
}

// hasMultiMembers reports whether the bracket can consume more than one character.
// Only a positive list with explicit collating symbols or equivalence classes can do that.
func (b *bracketSet) hasMultiMembers() bool {
	return !b.negated && (len(b.elems) > 0 || len(b.equivs) > 0)
}

// matchesMulti tests a multi-character candidate against the explicit elements and equivalence classes of a positive list.
func (b *bracketSet) matchesMulti(t []rune) bool {
	if len(t) > locale.MaxElementLength() {
		// No collating element is longer than the data's limit.
		return false
	}
	for _, e := range b.elems {
		if len(e) != len(t) {
			continue
		}
		all := true
		for i := range e {
			if !b.counterpartMatch(t[i], e[i]) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	if len(b.equivs) == 0 {
		return false
	}
	var candidate [maxElemAhead]rune
	return b.equivCandidate(t, candidate[:len(t)], 0)
}

// equivCandidate enumerates the case preimages of each position.
// It tests every candidate sequence for membership in some equivalence class.
// Without ICase, the only candidate is the subject sequence itself.
func (b *bracketSet) equivCandidate(t, candidate []rune, at int) bool {
	if at == len(t) {
		if !b.loc.IsCollatingElement(candidate) {
			return false
		}
		for _, eq := range b.equivs {
			if b.loc.PrimaryEqual(candidate, eq) {
				return true
			}
		}
		return false
	}
	candidate[at] = t[at]
	if b.equivCandidate(t, candidate, at+1) {
		return true
	}
	if !b.icase {
		return false
	}
	var buffer [maxPreimages]rune
	for _, m := range b.loc.AppendCasePreimages(buffer[:0], t[at]) {
		candidate[at] = m
		if b.equivCandidate(t, candidate, at+1) {
			return true
		}
	}
	return false
}

// matchesSpan tests whether the bracket accepts exactly the character span [i, j).
// The capture solver and the oracle share it.
func (b *bracketSet) matchesSpan(runes []rune, i, j int) bool {
	k := j - i
	if k < 1 {
		return false
	}
	if k == 1 {
		return b.matchesOne(runes[i])
	}
	if !b.hasMultiMembers() {
		return false
	}
	return b.matchesMulti(runes[i:j])
}
