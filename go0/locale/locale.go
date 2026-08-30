// Package locale supplies the LC_CTYPE and ERE-relevant LC_COLLATE operations that the POSIX ERE specification needs.
// It does not read the host locale database.
// The committed data comes from CLDR 48.2 and covers all 1,122 CLDR locales plus the POSIX locale.
//
// The data blob is embedded as a string.
// Every lookup reads fixed-width little-endian fields in place, and allocates nothing.
package locale

import (
	_ "embed"
	"slices"
	"strings"
)

//go:embed data.bin
var blob string

// Class identifies one of the twelve standard LC_CTYPE classes.
type Class uint8

const (
	Alnum Class = iota
	Alpha
	Blank
	Cntrl
	Digit
	Graph
	Lower
	Print
	Punct
	Space
	Upper
	Xdigit
	numClasses
)

// firstSequenceID is the element ID of the first multi-character collating element.
// Smaller IDs are Unicode scalar values.
const firstSequenceID = 0x110000

// Locale identifies one opened locale and collation selection.
// The zero value is not valid, so use Open or POSIX.
type Locale struct {
	localeIndex      uint16
	collationProfile uint16
	caseProfile      uint8
	posix            bool
	valid            bool
}

// POSIX returns the POSIX locale, also known as the C locale.
func POSIX() Locale {
	return Locale{posix: true, valid: true}
}

// IsPOSIX reports whether this locale is the POSIX locale.
func (l Locale) IsPOSIX() bool {
	return l.posix
}

// Valid reports whether this locale came from a successful Open or POSIX.
func (l Locale) Valid() bool {
	return l.valid
}

// section index constants match the generator's layout order.
const (
	secCtypeStage1 = iota
	secCtypeBlocks
	secCaseDefault
	secCaseTurkic
	secSeqCodepoints
	secSequences
	secRootContractions
	secRootEquivalences
	secCollationOverrides
	secContractionAdds
	secContractionRemoves
	secCollationProfiles
	secTypeNames
	secTypeNameOffsets
	secLocaleNames
	secLocaleNameOffsets
	secLocales
	secLocaleTypes
	secInvUpperDefault
	secInvLowerDefault
	secInvUpperTurkic
	secInvLowerTurkic
	secScalars
	numSections
)

var sections [numSections]struct{ off, end int }

func init() {
	const magic = "RVLOC001"
	if len(blob) < len(magic) || blob[:len(magic)] != magic {
		panic("locale: bad data blob")
	}
	cursor := len(magic)
	for i := range numSections {
		if cursor+4 > len(blob) {
			panic("locale: truncated data blob")
		}
		length := int(u32Raw(cursor))
		cursor += 4
		if cursor+length > len(blob) {
			panic("locale: truncated data blob")
		}
		sections[i].off = cursor
		sections[i].end = cursor + length
		cursor += length
	}
	if cursor != len(blob) {
		panic("locale: trailing bytes in data blob")
	}
	maxSeqLen = int(u32At(secScalars, 0))
}

// maxSeqLen caches the maximum sequence length of the scalar section.
// The match loops therefore never read the blob again for it.
var maxSeqLen int

func u32Raw(off int) uint32 {
	return uint32(blob[off]) | uint32(blob[off+1])<<8 |
		uint32(blob[off+2])<<16 | uint32(blob[off+3])<<24
}

func u16At(sec, index int) uint16 {
	off := sections[sec].off + 2*index
	return uint16(blob[off]) | uint16(blob[off+1])<<8
}

func u32At(sec, index int) uint32 {
	return u32Raw(sections[sec].off + 4*index)
}

func sectionLen(sec int) int {
	return sections[sec].end - sections[sec].off
}

func byteString(sec, off int) string {
	start := sections[sec].off + off
	rest := blob[start:sections[sec].end]
	before, _, ok := strings.Cut(rest, "\x00")
	if !ok {
		return rest
	}
	return before
}

func maxSequenceLength() int {
	return maxSeqLen
}

// MaxElementLength returns the largest character count of any multi-character collating element in the data.
func MaxElementLength() int {
	return maxSequenceLength()
}

func validScalar(r rune) bool {
	return r >= 0 && r <= 0x10ffff && !(r >= 0xd800 && r <= 0xdfff)
}

func asciiLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

const normalizedNameMax = 127

// normalizeName lowercases an ASCII locale name and maps '_' to '-'.
// It stops before an optional codeset or modifier.
// It also checks the codeset when the name carries one.
func normalizeName(input string) (string, bool) {
	if input == "" {
		return "", false
	}
	var out strings.Builder
	i := 0
	for i < len(input) && input[i] != '.' && input[i] != '@' {
		c := input[i]
		if c >= 0x80 || out.Len() == normalizedNameMax {
			return "", false
		}
		if c == '_' {
			c = '-'
		} else {
			c = asciiLower(c)
		}
		out.WriteByte(c)
		i++
	}
	if i < len(input) && input[i] == '.' {
		i++
		var codeset []byte
		for i < len(input) && input[i] != '@' {
			c := asciiLower(input[i])
			i++
			if len(codeset) == 5 || c >= 0x80 {
				return "", false
			}
			codeset = append(codeset, c)
		}
		if string(codeset) != "utf8" && string(codeset) != "utf-8" {
			return "", false
		}
	}
	return out.String(), true
}

// embeddedModifier extracts the value of a @collation= modifier, if the name has one.
func embeddedModifier(name string) (string, bool) {
	_, after, ok := strings.Cut(name, "@")
	if !ok {
		return "", false
	}
	modifier := after
	const keyword = "collation="
	if len(modifier) <= len(keyword) {
		return "", false
	}
	for i := range len(keyword) {
		if asciiLower(modifier[i]) != keyword[i] {
			return "", false
		}
	}
	return modifier[len(keyword):], true
}

func longTypeAlias(name string) string {
	switch name {
	case "dict":
		return "dictionary"
	case "phonebk":
		return "phonebook"
	case "trad":
		return "traditional"
	}
	return name
}

func normalizeType(input string) (string, bool) {
	if input == "" {
		return "", true
	}
	var out strings.Builder
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c >= 0x80 || out.Len() == normalizedNameMax {
			return "", false
		}
		out.WriteByte(asciiLower(c))
	}
	return longTypeAlias(out.String()), true
}

// findName does a binary search over a NUL-separated sorted name pool.
func findName(name string, poolSec, offsetsSec, count int) int {
	low, high := 0, count
	for low < high {
		middle := low + (high-low)/2
		entry := byteString(poolSec, int(u32At(offsetsSec, middle)))
		switch {
		case name == entry:
			return middle
		case name < entry:
			high = middle
		default:
			low = middle + 1
		}
	}
	return -1
}

func localesCount() int {
	return sectionLen(secLocales) / 20
}

// localeRow fields are name_id, type_first, type_count, case_profile and default_collation.
// The row on disk stores them as five u32 values.
func localeRow(index int) (typeFirst, typeCount int, caseProfile uint8, defaultCollation uint16) {
	base := 5 * index
	typeFirst = int(u32At(secLocales, base+1))
	typeCount = int(u32At(secLocales, base+2))
	caseProfile = uint8(u32At(secLocales, base+3))
	defaultCollation = uint16(u32At(secLocales, base+4))
	return
}

// Open resolves a locale name and optional collation type.
// Locale names are ASCII case-insensitive.
// They accept '-' or '_' separators, an optional .UTF-8 suffix, and an optional @collation=TYPE modifier.
// C and POSIX select the POSIX locale.
// The result is invalid when the name is unknown.
func Open(name, collationType string) (Locale, bool) {
	normalized, ok := normalizeName(name)
	if !ok {
		return Locale{}, false
	}
	modifier, hasModifier := embeddedModifier(name)
	if strings.IndexByte(name, '@') >= 0 && !hasModifier {
		return Locale{}, false
	}
	if hasModifier && collationType != "" {
		return Locale{}, false
	}
	if hasModifier {
		collationType = modifier
	}
	normalizedType, ok := normalizeType(collationType)
	if !ok {
		return Locale{}, false
	}

	if normalized == "c" || normalized == "posix" {
		if normalizedType != "" && normalizedType != "standard" {
			return Locale{}, false
		}
		return POSIX(), true
	}

	index := findName(normalized, secLocaleNames, secLocaleNameOffsets,
		localesCount())
	if index < 0 {
		return Locale{}, false
	}
	typeFirst, typeCount, caseProfile, defaultCollation := localeRow(index)
	result := Locale{
		localeIndex: uint16(index),
		caseProfile: caseProfile,
		valid:       true,
	}
	if normalizedType == "" {
		result.collationProfile = defaultCollation
		return result, true
	}

	typeNameCount := sectionLen(secTypeNameOffsets) / 4
	typeID := findName(normalizedType, secTypeNames, secTypeNameOffsets,
		typeNameCount)
	if typeID < 0 {
		return Locale{}, false
	}
	low, high := typeFirst, typeFirst+typeCount
	for low < high {
		middle := low + (high-low)/2
		rowTypeID := int(u16At(secLocaleTypes, 2*middle))
		if rowTypeID == typeID {
			result.collationProfile = u16At(secLocaleTypes, 2*middle+1)
			return result, true
		}
		if rowTypeID > typeID {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return Locale{}, false
}

// classNames lists the standard class names in Class value order.
var classNames = [numClasses]string{
	"alnum", "alpha", "blank", "cntrl", "digit", "graph",
	"lower", "print", "punct", "space", "upper", "xdigit",
}

// ClassByName maps a standard class name to its identifier.
func ClassByName(name string) (Class, bool) {
	index := slices.Index(classNames[:], name)
	if index < 0 {
		return 0, false
	}
	return Class(index), true
}

func posixMask(r rune) uint16 {
	upper := r >= 'A' && r <= 'Z'
	lower := r >= 'a' && r <= 'z'
	alpha := upper || lower
	digit := r >= '0' && r <= '9'
	alnum := alpha || digit
	blank := r == ' ' || r == '\t'
	space := blank || r == '\n' || r == '\v' || r == '\f' || r == '\r'
	cntrl := r <= 0x1f || r == 0x7f
	print := r >= 0x20 && r <= 0x7e
	graph := r >= 0x21 && r <= 0x7e
	punct := graph && !alnum
	xdigit := digit || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f')
	var mask uint16
	if alnum {
		mask |= 1 << Alnum
	}
	if alpha {
		mask |= 1 << Alpha
	}
	if blank {
		mask |= 1 << Blank
	}
	if cntrl {
		mask |= 1 << Cntrl
	}
	if digit {
		mask |= 1 << Digit
	}
	if graph {
		mask |= 1 << Graph
	}
	if lower {
		mask |= 1 << Lower
	}
	if print {
		mask |= 1 << Print
	}
	if punct {
		mask |= 1 << Punct
	}
	if space {
		mask |= 1 << Space
	}
	if upper {
		mask |= 1 << Upper
	}
	if xdigit {
		mask |= 1 << Xdigit
	}
	return mask
}

// ClassMask returns the set of standard LC_CTYPE classes that contain r, one bit per Class value.
func (l Locale) ClassMask(r rune) uint16 {
	if !l.valid || !validScalar(r) {
		return 0
	}
	if l.posix {
		return posixMask(r)
	}
	block := int(u16At(secCtypeStage1, int(r>>8)))
	return u16At(secCtypeBlocks, block*256+int(r&0xff))
}

// IsClass tests one Unicode scalar against a standard LC_CTYPE class.
func (l Locale) IsClass(class Class, r rune) bool {
	return class < numClasses && l.ClassMask(r)&(1<<class) != 0
}

// findCase does a binary search in a case-map section of u32 triples.
func findCase(sec int, r rune) (upper, lower rune, ok bool) {
	count := sectionLen(sec) / 12
	low, high := 0, count
	for low < high {
		middle := low + (high-low)/2
		cp := rune(u32At(sec, 3*middle))
		if cp == r {
			return rune(u32At(sec, 3*middle+1)), rune(u32At(sec, 3*middle+2)), true
		}
		if cp > r {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return 0, 0, false
}

func (l Locale) caseConvert(r rune, toUpper bool) rune {
	if !l.valid || !validScalar(r) {
		return r
	}
	if l.posix {
		if toUpper && r >= 'a' && r <= 'z' {
			return r - 32
		}
		if !toUpper && r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	if l.caseProfile == 1 {
		if upper, lower, ok := findCase(secCaseTurkic, r); ok {
			if toUpper {
				return upper
			}
			return lower
		}
	}
	upper, lower, ok := findCase(secCaseDefault, r)
	if !ok {
		return r
	}
	if toUpper {
		return upper
	}
	return lower
}

// appendPairSources appends every source whose pair target equals r.
// The section holds sorted (target, source) u32 pairs.
func appendPairSources(dst []rune, sec int, r rune) []rune {
	count := sectionLen(sec) / 8
	low, high := 0, count
	for low < high {
		middle := low + (high-low)/2
		if rune(u32At(sec, 2*middle)) < r {
			low = middle + 1
		} else {
			high = middle
		}
	}
	for ; low < count; low++ {
		if rune(u32At(sec, 2*low)) != r {
			break
		}
		dst = append(dst, rune(u32At(sec, 2*low+1)))
	}
	return dst
}

// AppendCasePreimages appends every scalar, other than r itself, whose uppercase or lowercase counterpart is r.
// The REG_ICASE closure rule needs these preimages.
// A subject character matches when some accepted character maps to it, and case mappings are not always involutions.
func (l Locale) AppendCasePreimages(dst []rune, r rune) []rune {
	if !l.valid || !validScalar(r) {
		return dst
	}
	if l.posix {
		if r >= 'A' && r <= 'Z' {
			return append(dst, r+32)
		}
		if r >= 'a' && r <= 'z' {
			return append(dst, r-32)
		}
		return dst
	}
	upperSec, lowerSec := secInvUpperDefault, secInvLowerDefault
	if l.caseProfile == 1 {
		upperSec, lowerSec = secInvUpperTurkic, secInvLowerTurkic
	}
	start := len(dst)
	dst = appendPairSources(dst, upperSec, r)
	dst = appendPairSources(dst, lowerSec, r)
	// Drop duplicates and r itself.
	// The appended runs are tiny.
	out := dst[:start]
	for _, candidate := range dst[start:] {
		if candidate != r && !slices.Contains(out[start:], candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

// ToUpper returns the locale's one-character uppercase counterpart, or the input itself.
func (l Locale) ToUpper(r rune) rune {
	return l.caseConvert(r, true)
}

// ToLower returns the locale's one-character lowercase counterpart, or the input itself.
func (l Locale) ToLower(r rune) rune {
	return l.caseConvert(r, false)
}

// compareSequence orders a scalar sequence against stored sequence index.
func compareSequence(seq []rune, index int) int {
	off := int(u32At(secSequences, 2*index))
	length := int(u32At(secSequences, 2*index+1))
	common := min(length, len(seq))
	for i := range common {
		stored := rune(u32At(secSeqCodepoints, off+i))
		if seq[i] < stored {
			return -1
		}
		if seq[i] > stored {
			return 1
		}
	}
	switch {
	case len(seq) < length:
		return -1
	case len(seq) > length:
		return 1
	}
	return 0
}

// elementID maps a scalar sequence to its collating-element ID.
// A single scalar maps to itself.
// A known multi-scalar sequence maps to firstSequenceID plus its table index.
func elementID(seq []rune) (uint32, bool) {
	if len(seq) == 0 {
		return 0, false
	}
	for _, r := range seq {
		if !validScalar(r) {
			return 0, false
		}
	}
	if len(seq) == 1 {
		return uint32(seq[0]), true
	}
	count := sectionLen(secSequences) / 8
	low, high := 0, count
	for low < high {
		middle := low + (high-low)/2
		comparison := compareSequence(seq, middle)
		if comparison == 0 {
			return uint32(firstSequenceID + middle), true
		}
		if comparison < 0 {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return 0, false
}

// u32Contains does a binary search in a slice of a sorted u32 section.
func u32Contains(sec, first, count int, needle uint32) bool {
	low, high := first, first+count
	for low < high {
		middle := low + (high-low)/2
		value := u32At(sec, middle)
		if value == needle {
			return true
		}
		if value > needle {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return false
}

// collationProfileRow fields are override_first, override_count, add_first, add_count, remove_first and remove_count.
func collationProfileRow(index int) (overrideFirst, overrideCount, addFirst, addCount, removeFirst, removeCount int) {
	base := 6 * index
	overrideFirst = int(u32At(secCollationProfiles, base))
	overrideCount = int(u32At(secCollationProfiles, base+1))
	addFirst = int(u32At(secCollationProfiles, base+2))
	addCount = int(u32At(secCollationProfiles, base+3))
	removeFirst = int(u32At(secCollationProfiles, base+4))
	removeCount = int(u32At(secCollationProfiles, base+5))
	return
}

func (l Locale) isContraction(element uint32) bool {
	if l.posix {
		return false
	}
	_, _, addFirst, addCount, removeFirst, removeCount :=
		collationProfileRow(int(l.collationProfile))
	if u32Contains(secContractionAdds, addFirst, addCount, element) {
		return true
	}
	rootCount := sectionLen(secRootContractions) / 4
	if !u32Contains(secRootContractions, 0, rootCount, element) {
		return false
	}
	return !u32Contains(secContractionRemoves, removeFirst, removeCount, element)
}

// collatingElementID maps seq to its element ID when seq is one collating element in this locale.
func (l Locale) collatingElementID(seq []rune) (uint32, bool) {
	element, ok := elementID(seq)
	if !ok {
		return 0, false
	}
	if len(seq) > 1 && !l.isContraction(element) {
		return 0, false
	}
	return element, true
}

// IsCollatingElement tests whether a scalar sequence is one collating element in this locale.
func (l Locale) IsCollatingElement(seq []rune) bool {
	if !l.valid {
		return false
	}
	_, ok := l.collatingElementID(seq)
	return ok
}

// CollatingPrefix returns the length in scalars of the longest collating-element prefix of seq.
// It returns zero for invalid input.
func (l Locale) CollatingPrefix(seq []rune) int {
	if !l.valid || len(seq) == 0 {
		return 0
	}
	maximum := min(len(seq), maxSequenceLength())
	for candidate := maximum; candidate >= 2; candidate-- {
		if l.IsCollatingElement(seq[:candidate]) {
			return candidate
		}
	}
	if l.IsCollatingElement(seq[:1]) {
		return 1
	}
	return 0
}

// findPair does a binary search in a section of sorted (element, representative) u32 pairs.
func findPair(sec, first, count int, element uint32) (uint32, bool) {
	low, high := first, first+count
	for low < high {
		middle := low + (high-low)/2
		key := u32At(sec, 2*middle)
		if key == element {
			return u32At(sec, 2*middle+1), true
		}
		if key > element {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return 0, false
}

func (l Locale) primaryToken(element uint32) uint64 {
	overrideFirst, overrideCount, _, _, _, _ :=
		collationProfileRow(int(l.collationProfile))
	if rep, ok := findPair(secCollationOverrides, overrideFirst, overrideCount,
		element); ok {
		return 0x2_0000_0000 | uint64(rep)
	}
	rootCount := sectionLen(secRootEquivalences) / 8
	if rep, ok := findPair(secRootEquivalences, 0, rootCount, element); ok {
		return 0x1_0000_0000 | uint64(rep)
	}
	return uint64(element)
}

// PrimaryEqual tests primary LC_COLLATE equivalence between two collating elements.
func (l Locale) PrimaryEqual(left, right []rune) bool {
	if !l.valid {
		return false
	}
	leftElement, ok := l.collatingElementID(left)
	if !ok {
		return false
	}
	rightElement, ok := l.collatingElementID(right)
	if !ok {
		return false
	}
	if leftElement == rightElement {
		return true
	}
	if l.posix {
		return false
	}
	return l.primaryToken(leftElement) == l.primaryToken(rightElement)
}

// MinEquivLength returns the smallest scalar count of any collating element whose primary weight equals the element seq in this locale.
// The bracket compiler uses it as the minimum length of an equivalence-class match.
func (l Locale) MinEquivLength(seq []rune) int {
	if !l.valid {
		return len(seq)
	}
	element, ok := l.collatingElementID(seq)
	if !ok {
		return len(seq)
	}
	if element < firstSequenceID {
		return 1
	}
	if l.posix {
		return len(seq)
	}
	// Every element that is not seq itself and shares its primary weight appears in the equivalence pair sections.
	// An unlisted element keeps its own ID as its token, which cannot collide.
	token := l.primaryToken(element)
	best := len(seq)
	overrideFirst, overrideCount, _, _, _, _ :=
		collationProfileRow(int(l.collationProfile))
	best = l.minTokenLength(secCollationOverrides, overrideFirst,
		overrideCount, token, best)
	if best == 1 {
		return 1
	}
	rootCount := sectionLen(secRootEquivalences) / 8
	return l.minTokenLength(secRootEquivalences, 0, rootCount, token, best)
}

// minTokenLength scans one (element, representative) pair section.
// It lowers best to the shortest collating element with the given token.
func (l Locale) minTokenLength(sec, first, count int, token uint64, best int) int {
	for i := first; i < first+count; i++ {
		candidate := u32At(sec, 2*i)
		if l.primaryToken(candidate) != token {
			continue
		}
		if candidate < firstSequenceID {
			return 1
		}
		if !l.isContraction(candidate) {
			continue
		}
		index := int(candidate - firstSequenceID)
		best = min(best, int(u32At(secSequences, 2*index+1)))
	}
	return best
}

// SupportsRanges reports whether bracket ranges are defined in this locale.
// Ranges in a non-POSIX locale use the permitted reject policy on purpose.
func (l Locale) SupportsRanges() bool {
	return l.valid && l.posix
}

// Count returns the generated CLDR locale count, without the C and POSIX aliases.
func Count() int {
	return localesCount()
}

// Name returns the normalized CLDR name at index, or "" when the index is out of range.
func Name(index int) string {
	if index < 0 || index >= localesCount() {
		return ""
	}
	return byteString(secLocaleNames, int(u32At(secLocaleNameOffsets, index)))
}
