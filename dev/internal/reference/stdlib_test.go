package reference

import (
	"math/rand"
	stdregexp "regexp"
	"sync"
	"testing"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

// genStdlibPattern builds patterns inside the subset that regexp.CompilePOSIX of Go also implements.
// That subset has no minimal modifiers, no collating syntax, and no locale classes beyond what RE2 knows.
func genStdlibPattern(rng *rand.Rand, depth int) string {
	if depth <= 0 {
		return genStdlibAtom(rng)
	}
	switch rng.Intn(9) {
	case 0:
		return genStdlibPattern(rng, depth-1) + "|" + genStdlibPattern(rng, depth-1)
	case 1:
		dup := ""
		switch rng.Intn(6) {
		case 0:
			dup = "*"
		case 1:
			dup = "+"
		case 2:
			dup = "?"
		case 3:
			dup = "{0,2}"
		case 4:
			dup = "{1,3}"
		}
		return "(" + genStdlibPattern(rng, depth-1) + ")" + dup
	case 2:
		return genStdlibPattern(rng, depth-1) + genStdlibPattern(rng, depth-1)
	case 3:
		return "^" + genStdlibPattern(rng, depth-1)
	case 4:
		return genStdlibPattern(rng, depth-1) + "$"
	default:
		atom := genStdlibAtom(rng)
		switch rng.Intn(5) {
		case 0:
			atom += "*"
		case 1:
			atom += "+"
		case 2:
			atom += "?"
		}
		return atom + genStdlibPattern(rng, depth-1)
	}
}

// genStdlibAtom avoids negated classes.
// The POSIX mode of Go keeps newline out of them, and POSIX does not.
func genStdlibAtom(rng *rand.Rand) string {
	switch rng.Intn(7) {
	case 0:
		return "."
	case 1:
		return "[ab]"
	case 2:
		return "[abc]"
	case 3:
		return "[a-c]"
	default:
		return string(rune('a' + rng.Intn(3)))
	}
}

// TestWholeMatchAgainstStdlib compares the selected whole match with the leftmost-longest engine of Go.
// It uses subjects too long for the exhaustive oracle.
// Only pmatch[0] is comparable, because RE2 does not promise POSIX captures.
func TestWholeMatchAgainstStdlib(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	tested := 0
	for range 1500 {
		pattern := genStdlibPattern(rng, 3)
		re, err := Compile(pattern, locale.POSIX(), 0)
		if err != nil {
			continue
		}
		// The subjects hold no newline.
		// The POSIX mode of Go gives dot, negated classes and anchors a newline-sensitive behavior that plain POSIX does not have.
		std, err := stdregexp.CompilePOSIX(pattern)
		if err != nil {
			continue
		}
		tested++
		for range 4 {
			subject := genSubject(rng, "abcab", 40+rng.Intn(160))
			pmatch := make([]Match, 1)
			ok, execErr := re.Exec(subject, pmatch, 0)
			if execErr != nil {
				t.Fatalf("%q on %q: %v", pattern, subject, execErr)
			}
			loc := std.FindStringIndex(subject)
			if ok != (loc != nil) {
				t.Fatalf("%q on %q: engine matched=%v, stdlib %v",
					pattern, subject, ok, loc)
			}
			if !ok {
				continue
			}
			if pmatch[0].So != loc[0] || pmatch[0].Eo != loc[1] {
				t.Fatalf("%q on %q: engine %v, stdlib %v",
					pattern, subject, pmatch[0], loc)
			}
		}
	}
	if tested < 1000 {
		t.Fatalf("too few comparable patterns: %d", tested)
	}
}

// TestConcurrentExec runs one compiled expression from many goroutines.
func TestConcurrentExec(t *testing.T) {
	re := compileOK(t, "(a|ab)(c|bcd)(d*)", 0)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 2000 {
				pmatch := make([]Match, 4)
				ok, err := re.Exec("xabcd", pmatch, 0)
				if err != nil || !ok {
					t.Errorf("concurrent Exec failed: %v %v", ok, err)
					return
				}
				// Rule 3 of section 4.3 resolves subpatterns left to right, and each one takes its longest compatible match.
				// Group 1 can take "ab" and still let the rest match.
				want := []Match{{1, 5}, {1, 3}, {3, 4}, {4, 5}}
				for idx := range want {
					if pmatch[idx] != want[idx] {
						t.Errorf("concurrent pmatch[%d] = %v", idx, pmatch[idx])
						return
					}
				}
			}
		})
	}
	wg.Wait()
}

// TestPhaseAAllocations checks the steady-state allocation count of the match-only path.
func TestPhaseAAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation changes allocation counts")
	}
	re := compileOK(t, "(a|b)*abb", 0)
	subject := "abababababababababababababababababababababababbz"
	pmatch := make([]Match, 1)
	// Warm the pool.
	for range 4 {
		if ok, err := re.Exec(subject, pmatch, 0); err != nil || !ok {
			t.Fatalf("warmup failed: %v %v", ok, err)
		}
	}
	allocs := testing.AllocsPerRun(200, func() {
		ok, err := re.Exec(subject, pmatch, 0)
		if err != nil || !ok {
			t.Fatalf("Exec failed: %v %v", ok, err)
		}
	})
	if allocs > 1 {
		t.Fatalf("match-only path allocates %.1f objects per run", allocs)
	}
}
