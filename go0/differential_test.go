package revera

import (
	"math/rand"
	"strings"
	"testing"

	"revera/locale"
)

// genPattern builds a random valid ERE over a small alphabet.
func genPattern(rng *rand.Rand, depth int) string {
	if depth <= 0 {
		return genAtom(rng)
	}
	switch rng.Intn(10) {
	case 0:
		return genPattern(rng, depth-1) + "|" + genPattern(rng, depth-1)
	case 1:
		return "(" + genPattern(rng, depth-1) + ")" + genDup(rng)
	case 2:
		return genPattern(rng, depth-1) + genPattern(rng, depth-1)
	case 3:
		atom := genAtom(rng)
		return atom + genDup(rng)
	case 4:
		return "^" + genPattern(rng, depth-1)
	case 5:
		return genPattern(rng, depth-1) + "$"
	default:
		return genAtom(rng) + genPattern(rng, depth-1)
	}
}

func genAtom(rng *rand.Rand) string {
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

func genDup(rng *rand.Rand) string {
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

func genSubject(rng *rand.Rand, alphabet string, maxLen int) string {
	length := rng.Intn(maxLen + 1)
	var out strings.Builder
	for range length {
		out.WriteByte(alphabet[rng.Intn(len(alphabet))])
	}
	return out.String()
}

func compareEngines(t *testing.T, re *Regexp, pattern, subject string, eflags ExecFlags) {
	t.Helper()
	got := make([]Match, re.NumSub()+1)
	want := make([]Match, re.NumSub()+1)
	okGot, errGot := re.Exec(subject, got, eflags)
	okWant, errWant := re.oracleFullExec(subject, want, eflags)
	if errGot != nil || errWant != nil {
		t.Fatalf("%q on %q: errors %v / %v", pattern, subject, errGot, errWant)
	}
	if okGot != okWant {
		t.Fatalf("%q on %q eflags=%d: engine matched=%v, oracle matched=%v",
			pattern, subject, eflags, okGot, okWant)
	}
	if !okGot {
		return
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("%q on %q eflags=%d: engine %v, oracle %v",
				pattern, subject, eflags, got, want)
		}
	}
}

func runDifferential(t *testing.T, seed int64, rounds int, cflags CompileFlags, alphabet string) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	for range rounds {
		pattern := genPattern(rng, 3)
		re, err := Compile(pattern, locale.POSIX(), cflags)
		if err != nil {
			// The generator can build rejected undefined spellings,
			// such as adjacent duplications via concatenation.
			continue
		}
		for range 6 {
			subject := genSubject(rng, alphabet, 7)
			var eflags ExecFlags
			switch rng.Intn(4) {
			case 1:
				eflags = NotBOL
			case 2:
				eflags = NotEOL
			case 3:
				eflags = NotBOL | NotEOL
			}
			compareEngines(t, re, pattern, subject, eflags)
		}
	}
}

func TestDifferentialPlain(t *testing.T) {
	runDifferential(t, 1, 3000, 0, "abc")
}

func TestDifferentialPlainNewlineSubjects(t *testing.T) {
	runDifferential(t, 6, 1000, 0, "ab\nc")
}

func TestDifferentialICase(t *testing.T) {
	runDifferential(t, 2, 1500, ICase, "abcABC")
}

func TestDifferentialNewline(t *testing.T) {
	runDifferential(t, 3, 1500, Newline, "ab\nc")
}

func TestDifferentialMinimal(t *testing.T) {
	runDifferential(t, 4, 1500, Minimal, "abc")
}

// FuzzDifferential drives arbitrary patterns and subjects through both
// engines and requires identical results.
func FuzzDifferential(f *testing.F) {
	f.Add("a|b*", "abab", uint8(0))
	f.Add("(a*)(a*?)c", "aaac", uint8(0))
	f.Add("[^a-c]?x{2,3}", "zxxx", uint8(1))
	f.Add("((a)|b)*", "ab\nb", uint8(2))
	f.Add("[[:alpha:]]+$", "ab c", uint8(3))
	f.Fuzz(func(t *testing.T, pattern, subject string, flags uint8) {
		if len(pattern) > 24 || len(subject) > 10 {
			return
		}
		cflags := CompileFlags(flags) & (ICase | Newline | Minimal)
		re, err := Compile(pattern, locale.POSIX(), cflags)
		if err != nil {
			return
		}
		got := make([]Match, re.NumSub()+1)
		want := make([]Match, re.NumSub()+1)
		okGot, errGot := re.Exec(subject, got, 0)
		okWant, errWant := re.oracleFullExec(subject, want, 0)
		if errGot != nil || errWant != nil {
			// Work-limit errors are acceptable; wrong answers are not.
			return
		}
		if okGot != okWant {
			t.Fatalf("%q on %q flags=%d: engine=%v oracle=%v",
				pattern, subject, cflags, okGot, okWant)
		}
		if !okGot {
			return
		}
		for idx := range want {
			if got[idx] != want[idx] {
				t.Fatalf("%q on %q flags=%d: engine %v, oracle %v",
					pattern, subject, cflags, got, want)
			}
		}
	})
}

func TestDifferentialMultiElement(t *testing.T) {
	cs, ok := locale.Open("cs", "")
	if !ok {
		t.Fatal("cs locale missing")
	}
	rng := rand.New(rand.NewSource(5))
	patterns := []string{
		"[[.ch.]]", "([[.ch.]]|c)h?", "[[.ch.]]*x?", "a?[[.ch.]]+",
		"([[.ch.]]?)(h*)", "[[=ch=]]", "([[.ch.]]|[ch])*",
	}
	for _, cflags := range []CompileFlags{0, ICase, Minimal} {
		for _, pattern := range patterns {
			re, err := Compile(pattern, cs, cflags)
			if err != nil {
				t.Fatalf("Compile(%q) failed: %v", pattern, err)
			}
			for range 40 {
				subject := genSubject(rng, "chxCH", 6)
				compareEngines(t, re, pattern, subject, 0)
			}
		}
	}
}
