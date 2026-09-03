package reference

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

func TestOnePassDetection(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{"(a)(b)", true},
		{"(a|b)*abb", true},
		{"((a)|b)*", true},
		{"(a|ab)", true},
		{"(.|ab)", true},
		{"^(ab)$", true},
		{"x{2,5}", true},
		{".*", true},
		{"([^a]*)b", true},
		{"((ab){1,3})c", true},
		{"(a|ab)(c|bcd)(d*)", false},
		{"(a*)(b*)", false},
		{"(a|.)", false},
		{"(a?)*", false},
	}
	for _, tc := range cases {
		re := compileOK(t, tc.pattern, 0)
		if re.onePass != tc.want {
			t.Errorf("%q: onePass = %v, want %v", tc.pattern, re.onePass, tc.want)
		}
	}

	cs, ok := locale.Open("cs", "")
	if !ok {
		t.Fatal("cs locale missing")
	}
	re, err := Compile("[[.ch.]]", cs, 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if re.onePass {
		t.Error("multi-character bracket must not be one-pass")
	}
}

func TestOnePassWalk(t *testing.T) {
	// The walk itself must succeed and apply the section 12.7 rule.
	// The last loop instance takes branch b, so the nested group (a) clears, even though an earlier instance set it.
	re := compileOK(t, "((a)|b)*", 0)
	if !re.onePass {
		t.Fatal("pattern must be one-pass")
	}
	d := decodeSubject("ab")
	caps := []Match{{-1, -1}, {-1, -1}, {-1, -1}}
	if !re.onePassCaps(&d, re.root, 0, 2, 0, caps) {
		t.Fatal("walk failed on a matching span")
	}
	if caps[1] != (Match{1, 2}) || caps[2] != (Match{-1, -1}) {
		t.Fatalf("caps = %v", caps)
	}
}

func TestOnePassInconsistencyFailsClosed(t *testing.T) {
	re := compileOK(t, "(a|.)", 0)
	if re.onePass {
		t.Fatal("ambiguous expression must not be one-pass")
	}
	re.onePass = true
	matched, err := re.Exec("a", make([]Match, re.NumSub()+1), 0)
	serr, ok := err.(*Error)
	if matched || !ok || serr.Code != ESpace {
		t.Fatalf("inconsistent one-pass walk returned matched=%v, error=%v", matched, err)
	}
}

// TestOnePassAgainstSolver drives random patterns through the one-pass walk, the phase B solver, and the oracle.
// It requires identical capture vectors from all three.
func TestOnePassAgainstSolver(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	eligible := 0
	for range 3000 {
		pattern := genPattern(rng, 3)
		re, err := Compile(pattern, locale.POSIX(), 0)
		if err != nil {
			continue
		}
		if !re.onePass || re.nsub == 0 {
			continue
		}
		eligible++
		for range 6 {
			subject := genSubject(rng, "abc", 7)
			fast := make([]Match, re.nsub+1)
			slow := make([]Match, re.nsub+1)
			okFast, errFast := re.Exec(subject, fast, 0)
			re.onePass = false
			okSlow, errSlow := re.Exec(subject, slow, 0)
			re.onePass = true
			if errFast != nil || errSlow != nil {
				t.Fatalf("%q on %q: errors %v / %v",
					pattern, subject, errFast, errSlow)
			}
			if okFast != okSlow {
				t.Fatalf("%q on %q: fast=%v solver=%v",
					pattern, subject, okFast, okSlow)
			}
			for idx := range fast {
				if okFast && fast[idx] != slow[idx] {
					t.Fatalf("%q on %q: fast %v, solver %v",
						pattern, subject, fast, slow)
				}
			}
			compareEngines(t, re, pattern, subject, 0)
		}
	}
	// The sweep must actually cover the fast path.
	if eligible < 100 {
		t.Fatalf("only %d eligible patterns; detection may have rotted", eligible)
	}
}

func TestOnePassAgainstSolverFlags(t *testing.T) {
	for _, cflags := range []CompileFlags{ICase, Newline, Minimal} {
		rng := rand.New(rand.NewSource(int64(8 + cflags)))
		for range 800 {
			pattern := genPattern(rng, 3)
			re, err := Compile(pattern, locale.POSIX(), cflags)
			if err != nil || !re.onePass || re.nsub == 0 {
				continue
			}
			for range 4 {
				subject := genSubject(rng, "abC\n", 7)
				compareEngines(t, re, pattern, subject, 0)
			}
		}
	}
}

func benchCaptureWalk(b *testing.B, onePass bool) {
	subject := strings.Repeat("ab", 2000) + "-abab"
	re, err := Compile("([ab]*)-([ab]{4})", locale.POSIX(), 0)
	if err != nil {
		b.Fatal(err)
	}
	re.onePass = onePass
	d := decodeSubject(subject)
	caps := make([]Match, 3)
	b.ReportAllocs()
	for b.Loop() {
		if serr := re.solveCaptures(&d, 0, len(d.runes), 0, caps); serr != nil {
			b.Fatal(serr)
		}
	}
}

// The pair below isolates phase B.
// The one-pass walk and the memoized solver each solve the same span.
func BenchmarkCaptureWalkOnePass(b *testing.B) {
	benchCaptureWalk(b, true)
}

func BenchmarkCaptureWalkSolver(b *testing.B) {
	benchCaptureWalk(b, false)
}

func BenchmarkCaptureOnePass(b *testing.B) {
	subject := strings.Repeat("ab", 2000) + "-abab"
	benchExec(b, "([ab]*)-([ab]{4})", subject, 3)
}
