package revera

import (
	"sort"
	"strings"
	"unicode/utf8"

	"revera/locale"
)

type runeRange struct{ lo, hi rune }

// bracketSet is the compiled form of one bracket expression.
type bracketSet struct {
	negated   bool
	ranges    []runeRange // sorted, non-overlapping single-character members
	classMask uint16      // union of standard LC_CTYPE classes
	elems     [][]rune    // explicit multi-character collating symbols
	equivs    [][]rune    // named equivalence-class elements
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
	b := &bracketSet{}
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
				// Non-POSIX-locale ranges use the permitted reject policy.
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

// parseBracketItem parses one list member: an ordinary character, a literal
// leading ']', a collating symbol, an equivalence class, or a character
// class. A single-character collating symbol becomes an ordinary character
// item, so it stays usable as a range endpoint.
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
				// This case is invalid, not undefined: the RE must be
				// rejected when the element does not exist.
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

// scanInner consumes "[X content X]" where X is '.', '=', or ':' and
// returns the content characters. emptyCode reports empty content.
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
	seq := make([]rune, 0, 4)
	for _, r := range content {
		seq = append(seq, r)
	}
	return seq, nil
}

// finalize sorts and merges the single-character ranges.
func (b *bracketSet) finalize() {
	if len(b.ranges) < 2 {
		return
	}
	sort.Slice(b.ranges, func(i, j int) bool {
		if b.ranges[i].lo != b.ranges[j].lo {
			return b.ranges[i].lo < b.ranges[j].lo
		}
		return b.ranges[i].hi < b.ranges[j].hi
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

// inRanges tests range membership with a binary search.
func (b *bracketSet) inRanges(c rune) bool {
	low, high := 0, len(b.ranges)
	for low < high {
		middle := low + (high-low)/2
		if c > b.ranges[middle].hi {
			low = middle + 1
		} else if c < b.ranges[middle].lo {
			high = middle
		} else {
			return true
		}
	}
	return false
}

// positiveSingle tests case-sensitive membership of one character in the
// positive list, ignoring multi-character elements.
func (b *bracketSet) positiveSingle(c rune, loc locale.Locale) bool {
	if b.inRanges(c) {
		return true
	}
	if b.classMask != 0 {
		for class := range locale.Class(12) {
			if b.classMask&(1<<class) != 0 && loc.IsClass(class, c) {
				return true
			}
		}
	}
	for _, eq := range b.equivs {
		single := [1]rune{c}
		if loc.PrimaryEqual(single[:], eq) {
			return true
		}
	}
	return false
}

// matchesOne tests whether the bracket accepts the single character c.
// Under ICase the closure applies after inversion for a negated list,
// exactly as section 10.2 requires.
func (b *bracketSet) matchesOne(c rune, loc locale.Locale, icase, newlineMode bool) bool {
	if c < 0 {
		// The invalid-byte sentinel matches nothing, not even a
		// negated list.
		return false
	}
	if b.negated {
		if newlineMode && c == '\n' {
			return false
		}
		if !b.positiveSingle(c, loc) {
			return true
		}
		if !icase {
			return false
		}
		var buffer [maxElemAhead]rune
		for _, m := range loc.AppendCasePreimages(buffer[:0], c) {
			if !b.positiveSingle(m, loc) {
				return true
			}
		}
		return false
	}
	if b.positiveSingle(c, loc) {
		return true
	}
	if !icase {
		return false
	}
	var buffer [maxElemAhead]rune
	for _, m := range loc.AppendCasePreimages(buffer[:0], c) {
		if b.positiveSingle(m, loc) {
			return true
		}
	}
	return false
}

// counterpartMatch tests one subject character against one element
// character under the ICase replacement rule.
func counterpartMatch(t, e rune, loc locale.Locale, icase bool) bool {
	if t == e {
		return true
	}
	if !icase {
		return false
	}
	return t == loc.ToUpper(e) || t == loc.ToLower(e)
}

// hasMultiMembers reports whether the bracket can consume more than one
// character. Only a positive list with explicit collating symbols or
// equivalence classes can.
func (b *bracketSet) hasMultiMembers() bool {
	return !b.negated && (len(b.elems) > 0 || len(b.equivs) > 0)
}

// matchesMulti tests a multi-character candidate against the explicit
// elements and equivalence classes of a positive list.
func (b *bracketSet) matchesMulti(t []rune, loc locale.Locale, icase bool) bool {
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
			if !counterpartMatch(t[i], e[i], loc, icase) {
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
	if !icase {
		if !loc.IsCollatingElement(t) {
			return false
		}
		for _, eq := range b.equivs {
			if loc.PrimaryEqual(t, eq) {
				return true
			}
		}
		return false
	}
	var candidate [maxElemAhead]rune
	return b.equivCandidate(t, candidate[:len(t)], 0, loc)
}

// equivCandidate enumerates case preimages per position and tests each
// candidate sequence for membership in some equivalence class.
func (b *bracketSet) equivCandidate(t, candidate []rune, at int, loc locale.Locale) bool {
	if at == len(t) {
		if !loc.IsCollatingElement(candidate) {
			return false
		}
		for _, eq := range b.equivs {
			if loc.PrimaryEqual(candidate, eq) {
				return true
			}
		}
		return false
	}
	candidate[at] = t[at]
	if b.equivCandidate(t, candidate, at+1, loc) {
		return true
	}
	var buffer [maxElemAhead]rune
	for _, m := range loc.AppendCasePreimages(buffer[:0], t[at]) {
		candidate[at] = m
		if b.equivCandidate(t, candidate, at+1, loc) {
			return true
		}
	}
	return false
}
