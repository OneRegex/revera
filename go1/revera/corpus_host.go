package revera

// Host file, outside the Vego subset. It holds the differential
// test corpus: the random pattern generator and the fixed tables.
// Both revera's differential tests and cmd/crosscheck consume it,
// so the Go-differential and the cross-language runs cover the
// same shapes.

import (
	"math/rand"
	"strings"
)

// GenPattern builds a random valid ERE over a small alphabet, the
// same shape the go0 differential uses.
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

// FixedPatterns drives a corpus of interesting patterns, including
// the spec section 16 shapes and the capacity fallbacks.
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
	// The full ((a*){250}){250} over a long subject needs tens
	// of gigabytes in both engines before ESpace, so the corpus
	// keeps the nesting but blocks the match with a final b.
	"((a*){250}){250}b",
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

var FixedFlagSets = []uint32{0, FlagICase, FlagNewline, FlagMinimal,
	FlagICase | FlagNewline, FlagNoSub}

// MultiElementPatterns exercise the cs locale's ch collating
// element.
var MultiElementPatterns = []string{
	"[[.ch.]]", "([[.ch.]]|c)h?", "[[.ch.]]*x?", "a?[[.ch.]]+",
	"([[.ch.]]?)(h*)", "[[=ch=]]", "([[.ch.]]|[ch])*",
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

var IterSubjects = []string{"", "abba", "b\nab", "aaa", "xyz"}
