package protocol

// Host file, outside the Vego subset.
// It holds the differential test corpus: the random pattern generator and the fixed tables.
// The differential tests of revera and cmd/crosscheck both read it.
// The Go-differential run and the cross-language runs therefore cover the same shapes.

import (
	"math/rand"
	"strings"

	"github.com/oneregex/revera/go"
)

// GenPattern builds a random valid ERE over a small alphabet.
// It has the same shape the differential tests of dev/internal/differential use.
func GenPattern(rng *rand.Rand, depth int) string {
	if depth <= 0 {
		return GenAtom(rng)
	}
	switch rng.Intn(10) {
	case 0:
		return GenPattern(rng, depth-1) + "|" + GenPattern(rng, depth-1)
	case 1:
		return "(" + GenPattern(rng, depth-1) + ")" + GenDup(rng)
	case 2:
		return GenPattern(rng, depth-1) + GenPattern(rng, depth-1)
	case 3:
		return GenAtom(rng) + GenDup(rng)
	case 4:
		return "^" + GenPattern(rng, depth-1)
	case 5:
		return GenPattern(rng, depth-1) + "$"
	default:
		return GenAtom(rng) + GenPattern(rng, depth-1)
	}
}

func GenAtom(rng *rand.Rand) string {
	switch rng.Intn(8) {
	case 0:
		return "."
	case 1:
		return "[ab]"
	case 2:
		return "[^a]"
	case 3:
		return "[a-c]"
	case 4:
		return "[[:alpha:]]"
	case 5:
		return "[[=a=]]"
	default:
		return string(rune('a' + rng.Intn(3)))
	}
}

func GenDup(rng *rand.Rand) string {
	var dup string
	switch rng.Intn(7) {
	case 0:
		dup = "*"
	case 1:
		dup = "+"
	case 2:
		dup = "?"
	case 3:
		dup = "{2}"
	case 4:
		dup = "{0,2}"
	case 5:
		dup = "{1,}"
	default:
		return ""
	}
	if rng.Intn(3) == 0 {
		dup += "?"
	}
	return dup
}

func GenSubject(rng *rand.Rand, alphabet string, maxLen int) string {
	length := rng.Intn(maxLen + 1)
	var out strings.Builder
	for range length {
		out.WriteByte(alphabet[rng.Intn(len(alphabet))])
	}
	return out.String()
}

// HeavyPattern is the fixed pattern whose executions cost the most.
// The full ((a*){250}){250} over a long subject needs tens of gigabytes in both engines before ESpace.
// The corpus therefore keeps the nesting, but blocks the match with a final b.
// Each execution still costs tens of milliseconds, so the light corpus and the fuzz seeds leave them out.
const HeavyPattern = "((a*){250}){250}b"

// FixedPatterns drives a corpus of interesting patterns.
// It includes the spec section 16 shapes and the capacity fallbacks.
var FixedPatterns = []string{
	"(a|ab)(c|bcd)(d*)",
	"(a*)(a*?)c",
	"((a)|b)*",
	"(a?)*b",
	"(a{0,2}){2,3}",
	"x{2,3}",
	"(wee|week)(knights|night)",
	"a[bc]d",
	"[[:alpha:]]+$",
	"^[^a]*$",
	"(a+)(b+)?",
	"(a|b)*abb",
	"\\.\\[\\(",
	"a{0}b",
	"(|a)b",
	"a{251}{250}{250}",
	"(a{200}){200}{200}",
	HeavyPattern,
	"((a*){4}){4}",
	"[]a]b",
	"[^]a]b",
	"[a-]b",
	"ab$c",
	"^*a",
	"a**",
	"a{2,1}",
	"a{",
	"a\\",
	"(ab",
	"ab)",
	"[ab",
	"a{1000}",
	"é(è|e)*",
	".€.",
}

var FixedSubjects = []string{
	"", "a", "b", "ab", "abc", "abcd", "aabb", "abab", "aaac",
	"xxx", "weeknights", "a\nb", "\n", "aaaaab", "abb", "aabbab",
	"éèe", "x€y", "\xff", "a\xffb", "a\x00b",
	strings.Repeat("a", 120),
}

var FixedFlagSets = []uint32{0, revera.FlagICase, revera.FlagNewline, revera.FlagMinimal,
	revera.FlagICase | revera.FlagNewline, revera.FlagNoSub}

// MultiElementPatterns exercise the ch collating element of the cs locale.
var MultiElementPatterns = []string{
	"[[.ch.]]", "([[.ch.]]|c)h?", "[[.ch.]]*x?", "a?[[.ch.]]+",
	"([[.ch.]]?)(h*)", "[[=ch=]]", "([[.ch.]]|[ch])*", "([[=ch=]]|c)h?",
}

// ReplaceCase is one ReplaceAll differential call.
type ReplaceCase struct {
	Pattern     string
	Subject     string
	Replacement string
	Limit       int
}

var ReplaceCases = []ReplaceCase{
	{"(a+)(b+)", "aabb xab", `\2\1`, -1},
	{"a*", "bab", "-", -1},
	{"a*", "bab", "&&", 2},
	{"b", "abc", `[\&]`, -1},
	{"x", "abc", "y", -1},
	{"(a)(b)?", "ab a", `<\1\2>`, -1},
	{"a", "aaaa", "z", 2},
	{"a", "aaaa", "z", 0},
}

var IterPatterns = []string{"a*", "(a|b)+", "b", "", "a?"}

// LocalePatterns and LocaleSubjects exercise case-insensitive matching under locales with distinct case behavior.
// The subjects hold dotted and dotless i, sharp s, and final sigma.
var LocalePatterns = []string{"i+", "k", "[[:alpha:]]+", "s(a|s)*", "(σ|i)+"}

// LocaleBracketPatterns exercise the locale lookups of bracket tests.
var LocaleBracketPatterns = []string{
	"[[=a=]]", "[[=a=][=e=][=s=]]+", "[^[=i=]]*x", "[ab[=i=]]+x", "([[=s=]]|s)+", "[[.a.][=c=]]h?",
}

// LocaleBracketSubjects are long enough for the bracket tests to dominate the contract check.
var LocaleBracketSubjects = []string{
	strings.Repeat("z", 64), strings.Repeat("sS", 32) + "ſ", strings.Repeat("ıİiI", 16),
	strings.Repeat("äÄaA", 16), strings.Repeat("ch", 32),
}

// LocaleBracketShortSubjects keep the case-insensitive runs cheap, since their probes try every case candidate.
var LocaleBracketShortSubjects = []string{"zzzz", "sSſs", "ıİiI", "äÄaA", "chCH"}

var LocaleSubjects = []string{
	"IİıiIi", "KkK", "straße", "SS ss ſ", "ΣΟΦΟΣ σοφος τέλος ς",
	"Iİıi", "ABC abc", "",
}

var IterSubjects = []string{"", "abba", "b\nab", "aaa", "xyz"}
