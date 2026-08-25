package revera

// Differential tests: the Vego rewrite must agree with the go0
// engine on everything observable. go0 is itself validated against
// an enumerating reference matcher and against host regcomp(), so
// agreement with go0 carries that assurance over.

import (
	"math/rand"
	"testing"

	g0 "revera"
	g0loc "revera/locale"
)

// g0Code maps a go0 error to its numeric code, with 0 for success.
func g0Code(err error) int32 {
	if err == nil {
		return 0
	}
	return int32(err.(*g0.Error).Code)
}

func compileBoth(t *testing.T, pattern string, flags uint32) (*g0.Regexp, Regexp, bool) {
	t.Helper()
	re0, err0 := g0.Compile(pattern, g0loc.POSIX(), g0.CompileFlags(flags))
	re1, err1 := Compile(pattern, LocalePOSIX(), flags)
	if (err0 == nil) != (err1.Code == ErrNone) {
		t.Fatalf("Compile(%q, %d): go0 err=%v, go1 code=%d", pattern, flags, err0, err1.Code)
	}
	if err0 != nil {
		if g0Code(err0) != err1.Code || err0.(*g0.Error).Pos != err1.Pos {
			t.Fatalf("Compile(%q, %d): go0 (%d,%d), go1 (%d,%d)",
				pattern, flags, g0Code(err0), err0.(*g0.Error).Pos, err1.Code, err1.Pos)
		}
		return nil, re1, false
	}
	if re0.NumSub() != NumSub(&re1) {
		t.Fatalf("Compile(%q): NumSub go0=%d go1=%d", pattern, re0.NumSub(), NumSub(&re1))
	}
	return re0, re1, true
}

func compareExec(t *testing.T, pattern string, re0 *g0.Regexp, re1 *Regexp, subject string, eflags uint32) {
	t.Helper()
	got0 := make([]g0.Match, re0.NumSub()+1)
	got1 := make([]Match, NumSub(re1)+1)
	ok0, err0 := re0.Exec(subject, got0, g0.ExecFlags(eflags))
	ok1, err1 := Exec(re1, subject, got1, eflags)
	code0 := g0Code(err0)
	if code0 != err1.Code {
		t.Fatalf("%q on %q eflags=%d: go0 code=%d, go1 code=%d",
			pattern, subject, eflags, code0, err1.Code)
	}
	if err0 != nil {
		return
	}
	if ok0 != ok1 {
		t.Fatalf("%q on %q eflags=%d: go0 matched=%v, go1 matched=%v",
			pattern, subject, eflags, ok0, ok1)
	}
	if !ok0 {
		return
	}
	for idx := range got0 {
		if got0[idx].So != got1[idx].So || got0[idx].Eo != got1[idx].Eo {
			t.Fatalf("%q on %q eflags=%d: go0 %v, go1 %v",
				pattern, subject, eflags, got0, got1)
		}
	}
}

func runDifferential(t *testing.T, seed int64, rounds int, cflags uint32, alphabet string) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	for range rounds {
		pattern := GenPattern(rng, 3)
		re0, re1, ok := compileBoth(t, pattern, cflags)
		if !ok {
			continue
		}
		for range 6 {
			subject := GenSubject(rng, alphabet, 7)
			eflags := uint32(rng.Intn(4))
			compareExec(t, pattern, re0, &re1, subject, eflags)
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
	runDifferential(t, 2, 1500, FlagICase, "abcABC")
}

func TestDifferentialNewline(t *testing.T) {
	runDifferential(t, 3, 1500, FlagNewline, "ab\nc")
}

func TestDifferentialMinimal(t *testing.T) {
	runDifferential(t, 4, 1500, FlagMinimal, "abc")
}

func TestDifferentialICaseMinimal(t *testing.T) {
	runDifferential(t, 7, 1000, FlagICase|FlagMinimal, "abAB")
}

// TestDifferentialFixed drives a corpus of interesting patterns,
// including the spec section 16 shapes and the capacity fallbacks,
// through both engines.
func TestDifferentialFixed(t *testing.T) {
	for _, cflags := range FixedFlagSets {
		for _, pattern := range FixedPatterns {
			re0, re1, ok := compileBoth(t, pattern, cflags)
			if !ok {
				continue
			}
			for _, subject := range FixedSubjects {
				for _, eflags := range []uint32{0, 1, 2, 3} {
					compareExec(t, pattern, re0, &re1, subject, eflags)
				}
			}
		}
	}
}

func TestDifferentialMultiElement(t *testing.T) {
	cs0, ok := g0loc.Open("cs", "")
	if !ok {
		t.Fatal("go0 cs locale missing")
	}
	cs1, ok1 := Open("cs", "")
	if !ok1 {
		t.Fatal("go1 cs locale missing")
	}
	rng := rand.New(rand.NewSource(5))
	for _, cflags := range []uint32{0, FlagICase, FlagMinimal} {
		for _, pattern := range MultiElementPatterns {
			re0, err0 := g0.Compile(pattern, cs0, g0.CompileFlags(cflags))
			if err0 != nil {
				t.Fatalf("go0 Compile(%q) failed: %v", pattern, err0)
			}
			re1, err1 := Compile(pattern, cs1, cflags)
			if err1.Code != ErrNone {
				t.Fatalf("go1 Compile(%q) failed: code %d", pattern, err1.Code)
			}
			for range 40 {
				subject := GenSubject(rng, "chxCH", 6)
				compareExec(t, pattern, re0, &re1, subject, 0)
			}
		}
	}
}

func TestDifferentialReplace(t *testing.T) {
	for _, c := range ReplaceCases {
		re0, re1, ok := compileBoth(t, c.Pattern, 0)
		if !ok {
			t.Fatalf("corpus pattern %q failed to compile", c.Pattern)
		}
		out0, err0 := re0.ReplaceAll(c.Subject, c.Replacement, c.Limit, 0)
		out1, err1 := ReplaceAll(&re1, c.Subject, c.Replacement, c.Limit, 0)
		code0 := g0Code(err0)
		if code0 != err1.Code {
			t.Fatalf("ReplaceAll(%q, %q): go0 code=%d, go1 code=%d",
				c.Pattern, c.Replacement, code0, err1.Code)
		}
		if err0 == nil && out0 != out1 {
			t.Fatalf("ReplaceAll(%q, %q, %q): go0 %q, go1 %q",
				c.Pattern, c.Subject, c.Replacement, out0, out1)
		}
	}
}

func TestDifferentialMatchIter(t *testing.T) {
	for _, pattern := range IterPatterns {
		if pattern == "" {
			continue
		}
		for _, subject := range IterSubjects {
			for _, limit := range []int{-1, 0, 1, 2} {
				re0, re1, ok := compileBoth(t, pattern, 0)
				if !ok {
					continue
				}
				var spans0 [][]g0.Match
				err0 := re0.MatchAll(subject, limit, 0, func(pmatch []g0.Match) bool {
					row := make([]g0.Match, len(pmatch))
					copy(row, pmatch)
					spans0 = append(spans0, row)
					return true
				})
				if err0 != nil {
					t.Fatalf("go0 MatchAll(%q, %q): %v", pattern, subject, err0)
				}
				it, ierr := MatchIterInit(&re1, limit)
				if ierr.Code != ErrNone {
					t.Fatalf("go1 MatchIterInit: code %d", ierr.Code)
				}
				pmatch := make([]Match, NumSub(&re1)+1)
				var spans1 [][]Match
				for {
					got, nerr := MatchIterNext(&re1, &it, subject, 0, pmatch)
					if nerr.Code != ErrNone {
						t.Fatalf("go1 MatchIterNext: code %d", nerr.Code)
					}
					if !got {
						break
					}
					row := make([]Match, len(pmatch))
					copy(row, pmatch)
					spans1 = append(spans1, row)
				}
				if len(spans0) != len(spans1) {
					t.Fatalf("MatchAll(%q, %q, limit=%d): go0 %d matches, go1 %d",
						pattern, subject, limit, len(spans0), len(spans1))
				}
				for i := range spans0 {
					for k := range spans0[i] {
						if spans0[i][k].So != spans1[i][k].So ||
							spans0[i][k].Eo != spans1[i][k].Eo {
							t.Fatalf("MatchAll(%q, %q): row %d go0 %v go1 %v",
								pattern, subject, i, spans0[i], spans1[i])
						}
					}
				}
			}
		}
	}
}

func TestErrorText(t *testing.T) {
	if ErrorText(ErrNone) != "success" ||
		ErrorText(ErrESpace) != "capacity limit reached" ||
		ErrorText(99) != "unknown error" {
		t.Fatal("unexpected error text")
	}
}

func TestContractSmoke(t *testing.T) {
	re, c, err := CompileWithContract("(a|ab)(c|bcd)(d*)", LocalePOSIX(), 0, 1<<12)
	if err.Code != ErrNone {
		t.Fatalf("CompileWithContract failed: code %d", err.Code)
	}
	if !c.HasSolver {
		t.Fatal("expected a solver contract")
	}
	if ContractHeapBytes(&c) <= 0 || ContractStackBytes(&c) <= 0 ||
		ContractSteps(&c) <= 0 {
		t.Fatalf("contract has nonpositive figures: %+v", c)
	}
	small := ContractFor(&re, 16)
	if ContractSteps(&small) > ContractSteps(&c) {
		t.Fatal("contract steps must grow with the input bound")
	}
}
