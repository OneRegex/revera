//go:build cgo && darwin

package reference

// This file automates the libc differential harness that used to live in tmp/ and run by hand.
// It generates the same 20,000 pattern and subject pairs.
// It compares full capture vectors against the host regcomp and regexec.
// The macOS libc is the checked reference.
// Other libc implementations diverge from POSIX in ways this harness does not classify.

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/oneregex/revera/dev/internal/reference/libcre"
	"github.com/oneregex/revera/dev/internal/reference/locale"
)

func libcAtom(rng *rand.Rand) string {
	switch rng.Intn(9) {
	case 0:
		return "."
	case 1:
		return "[ab]"
	case 2:
		return "[a-c]"
	case 3:
		return "[^ab]"
	case 4:
		return "[^a-c]"
	default:
		return string(rune('a' + rng.Intn(3)))
	}
}

func libcPattern(rng *rand.Rand, depth int) string {
	if depth <= 0 {
		return libcAtom(rng)
	}
	switch rng.Intn(11) {
	case 9:
		return "^" + libcPattern(rng, depth-1)
	case 10:
		return libcPattern(rng, depth-1) + "$"
	case 0:
		return libcPattern(rng, depth-1) + "|" + libcPattern(rng, depth-1)
	case 1:
		dups := []string{"", "*", "+", "?", "{2}", "{0,2}", "{1,}"}
		return "(" + libcPattern(rng, depth-1) + ")" + dups[rng.Intn(len(dups))]
	case 2:
		return libcPattern(rng, depth-1) + libcPattern(rng, depth-1)
	default:
		return libcAtom(rng) + libcPattern(rng, depth-1)
	}
}

// knownDivergence sorts a result into the two documented libc divergence classes.
// Anything outside them is a real failure.
func knownDivergence(pattern string, ours []Match, theirs [][2]int) bool {
	// libc skips a leftmost empty match and takes a later non-empty one.
	// Rule 1 of section 4.3 selects the earlier empty match.
	if ours[0].So == ours[0].Eo && theirs[0][0] > ours[0].So {
		return true
	}
	// libc reports nonparticipation for {0,n}-style intervals, but empty participation for *.
	// The spec states one rule for both, so this implementation stays uniform.
	if !strings.Contains(pattern, "{0") {
		return false
	}
	if theirs[0] != [2]int{ours[0].So, ours[0].Eo} {
		return false
	}
	for idx := 1; idx < len(ours) && idx < len(theirs); idx++ {
		got, want := ours[idx], theirs[idx]
		if got == (Match{want[0], want[1]}) {
			continue
		}
		if want != [2]int{-1, -1} || got.So != got.Eo {
			return false
		}
	}
	return true
}

func TestLibcDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	divergences := 0
	for range 20000 {
		pattern := libcPattern(rng, 3)
		var b strings.Builder
		for range rng.Intn(9) {
			b.WriteByte(byte('a' + rng.Intn(3)))
		}
		subject := b.String()

		theirs := libcre.Run(pattern, subject, 10)
		re, err := Compile(pattern, locale.POSIX(), 0)
		if err != nil {
			if theirs.Compiled {
				t.Errorf("compile %q: we reject (%v), libc accepts", pattern, err)
			}
			continue
		}
		if !theirs.Compiled {
			t.Errorf("compile %q: we accept, libc rejects", pattern)
			continue
		}
		pmatch := make([]Match, re.NumSub()+1)
		matched, execErr := re.Exec(subject, pmatch, 0)
		if execErr != nil {
			t.Errorf("exec %q on %q: %v", pattern, subject, execErr)
			continue
		}
		if matched != theirs.Matched {
			t.Errorf("%q on %q: matched %v, libc %v",
				pattern, subject, matched, theirs.Matched)
			continue
		}
		if !matched || equalSpans(pmatch, theirs.Spans) {
			continue
		}
		if knownDivergence(pattern, pmatch, theirs.Spans) {
			divergences++
			continue
		}
		t.Errorf("%q on %q: we %v, libc %v", pattern, subject, pmatch, theirs.Spans)
	}
	// The corpus is seeded, so the count is exact: 14 interval participation cases and 4 leftmost-empty cases.
	// A drop means the implementation started to copy a libc bug.
	// A rise means new unclassified behavior slipped into a known class.
	if divergences != 18 {
		t.Errorf("known divergences drifted: %d cases, want 18", divergences)
	}
}

// TestLibcKnownDivergences pins one example per documented divergence class, on both sides.
// The assertions on our side fail if the implementation moves toward the libc behavior.
// The assertions on the libc side record what the host really returns.
func TestLibcKnownDivergences(t *testing.T) {
	cases := []struct {
		pattern, subject string
		ours             []Match
		libc             [][2]int
	}{
		// libc drops participation for {0,n} where it keeps it for *.
		{"[ab]((b)?){0,2}", "aaa",
			[]Match{{0, 1}, {1, 1}, {-1, -1}},
			[][2]int{{0, 1}, {-1, -1}, {-1, -1}}},
		{"((ba){0,2}){0,2}", "bbaaa",
			[]Match{{0, 0}, {0, 0}, {-1, -1}},
			[][2]int{{0, 0}, {-1, -1}, {-1, -1}}},
		// libc skips the leftmost empty match and takes a later non-empty one.
		{"((c){0,2}$)?", "caac",
			[]Match{{0, 0}, {-1, -1}, {-1, -1}},
			[][2]int{{3, 4}, {3, 4}, {3, 4}}},
	}
	for _, tc := range cases {
		re, err := Compile(tc.pattern, locale.POSIX(), 0)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.pattern, err)
		}
		pmatch := make([]Match, re.NumSub()+1)
		matched, execErr := re.Exec(tc.subject, pmatch, 0)
		if execErr != nil || !matched {
			t.Fatalf("%q on %q: %v %v", tc.pattern, tc.subject, matched, execErr)
		}
		for idx := range tc.ours {
			if pmatch[idx] != tc.ours[idx] {
				t.Errorf("%q on %q: ours %v, want %v",
					tc.pattern, tc.subject, pmatch, tc.ours)
				break
			}
		}
		theirs := libcre.Run(tc.pattern, tc.subject, len(tc.libc))
		if !theirs.Compiled || !theirs.Matched {
			t.Fatalf("%q on %q: libc %+v", tc.pattern, tc.subject, theirs)
		}
		for idx := range tc.libc {
			if theirs.Spans[idx] != tc.libc[idx] {
				t.Errorf("%q on %q: libc %v, documented %v",
					tc.pattern, tc.subject, theirs.Spans, tc.libc)
				break
			}
		}
	}
}

func equalSpans(ours []Match, theirs [][2]int) bool {
	for idx := range ours {
		if idx >= len(theirs) {
			return true
		}
		if ours[idx] != (Match{theirs[idx][0], theirs[idx][1]}) {
			return false
		}
	}
	return true
}
