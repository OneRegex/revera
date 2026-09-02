// Package differential compares the Revera engine with the reference engine on everything observable.
// An enumerating reference matcher and the host regcomp() both check the reference engine itself.
// Agreement with it therefore carries that assurance over.
package differential

import (
	"math/rand"
	"testing"

	"github.com/oneregex/revera/dev/internal/protocol"
	"github.com/oneregex/revera/dev/internal/reference"
	"github.com/oneregex/revera/dev/internal/reference/locale"
	"github.com/oneregex/revera/go"
)

// referenceCode maps a reference engine error to its numeric code, with 0 for success.
func referenceCode(err error) int32 {
	if err == nil {
		return 0
	}
	return int32(err.(*reference.Error).Code)
}

func compileBoth(t *testing.T, pattern string, flags uint32) (*reference.Regexp, revera.Regexp, bool) {
	t.Helper()
	re0, err0 := reference.Compile(pattern, locale.POSIX(), reference.CompileFlags(flags))
	re1, err1 := revera.Compile(pattern, revera.LocalePOSIX(), flags)
	if (err0 == nil) != (err1.Code == revera.ErrNone) {
		t.Fatalf("Compile(%q, %d): reference err=%v, revera code=%d", pattern, flags, err0, err1.Code)
	}
	if err0 != nil {
		if referenceCode(err0) != err1.Code || err0.(*reference.Error).Pos != err1.Pos {
			t.Fatalf("Compile(%q, %d): reference (%d,%d), revera (%d,%d)",
				pattern, flags, referenceCode(err0), err0.(*reference.Error).Pos, err1.Code, err1.Pos)
		}
		return nil, re1, false
	}
	if re0.NumSub() != revera.NumSub(&re1) {
		t.Fatalf("Compile(%q): NumSub reference=%d revera=%d", pattern, re0.NumSub(), revera.NumSub(&re1))
	}
	return re0, re1, true
}

func compareExec(t *testing.T, pattern string, re0 *reference.Regexp, re1 *revera.Regexp, subject string, eflags uint32) {
	t.Helper()
	got0 := make([]reference.Match, re0.NumSub()+1)
	got1 := make([]revera.Match, revera.NumSub(re1)+1)
	ok0, err0 := re0.Exec(subject, got0, reference.ExecFlags(eflags))
	ok1, err1 := revera.Exec(re1, subject, got1, eflags)
	code0 := referenceCode(err0)
	if code0 != err1.Code {
		t.Fatalf("%q on %q eflags=%d: reference code=%d, revera code=%d",
			pattern, subject, eflags, code0, err1.Code)
	}
	if err0 != nil {
		return
	}
	if ok0 != ok1 {
		t.Fatalf("%q on %q eflags=%d: reference matched=%v, revera matched=%v",
			pattern, subject, eflags, ok0, ok1)
	}
	if !ok0 {
		return
	}
	for idx := range got0 {
		if got0[idx].So != got1[idx].So || got0[idx].Eo != got1[idx].Eo {
			t.Fatalf("%q on %q eflags=%d: reference %v, revera %v",
				pattern, subject, eflags, got0, got1)
		}
	}
}

func runDifferential(t *testing.T, seed int64, rounds int, cflags uint32, alphabet string) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	for range rounds {
		pattern := protocol.GenPattern(rng, 3)
		re0, re1, ok := compileBoth(t, pattern, cflags)
		if !ok {
			continue
		}
		for range 6 {
			subject := protocol.GenSubject(rng, alphabet, 7)
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
	runDifferential(t, 2, 1500, revera.FlagICase, "abcABC")
}

func TestDifferentialNewline(t *testing.T) {
	runDifferential(t, 3, 1500, revera.FlagNewline, "ab\nc")
}

func TestDifferentialMinimal(t *testing.T) {
	runDifferential(t, 4, 1500, revera.FlagMinimal, "abc")
}

func TestDifferentialICaseMinimal(t *testing.T) {
	runDifferential(t, 7, 1000, revera.FlagICase|revera.FlagMinimal, "abAB")
}

// TestDifferentialFixed drives a corpus of interesting patterns through both engines.
// The corpus includes the spec section 16 shapes and the capacity fallbacks.
func TestDifferentialFixed(t *testing.T) {
	for _, cflags := range protocol.FixedFlagSets {
		for _, pattern := range protocol.FixedPatterns {
			re0, re1, ok := compileBoth(t, pattern, cflags)
			if !ok {
				continue
			}
			for _, subject := range protocol.FixedSubjects {
				for _, eflags := range []uint32{0, 1, 2, 3} {
					compareExec(t, pattern, re0, &re1, subject, eflags)
				}
			}
		}
	}
}

func TestDifferentialMultiElement(t *testing.T) {
	cs0, ok := locale.Open("cs", "")
	if !ok {
		t.Fatal("reference cs locale missing")
	}
	cs1, err1 := revera.OpenLocale("cs", "")
	if err1 != nil {
		t.Fatal("revera cs locale missing")
	}
	rng := rand.New(rand.NewSource(5))
	for _, cflags := range []uint32{0, revera.FlagICase, revera.FlagMinimal} {
		for _, pattern := range protocol.MultiElementPatterns {
			re0, err0 := reference.Compile(pattern, cs0, reference.CompileFlags(cflags))
			if err0 != nil {
				t.Fatalf("reference Compile(%q) failed: %v", pattern, err0)
			}
			re1, err1 := revera.Compile(pattern, cs1, cflags)
			if err1.Code != revera.ErrNone {
				t.Fatalf("revera Compile(%q) failed: code %d", pattern, err1.Code)
			}
			for range 40 {
				subject := protocol.GenSubject(rng, "chxCH", 6)
				compareExec(t, pattern, re0, &re1, subject, 0)
			}
		}
	}
}

func TestDifferentialReplace(t *testing.T) {
	for _, c := range protocol.ReplaceCases {
		re0, re1, ok := compileBoth(t, c.Pattern, 0)
		if !ok {
			t.Fatalf("corpus pattern %q failed to compile", c.Pattern)
		}
		out0, err0 := re0.ReplaceAll(c.Subject, c.Replacement, c.Limit, 0)
		out1, err1 := revera.ReplaceAll(&re1, c.Subject, c.Replacement, c.Limit, 0)
		code0 := referenceCode(err0)
		if code0 != err1.Code {
			t.Fatalf("ReplaceAll(%q, %q): reference code=%d, revera code=%d",
				c.Pattern, c.Replacement, code0, err1.Code)
		}
		if err0 == nil && out0 != out1 {
			t.Fatalf("ReplaceAll(%q, %q, %q): reference %q, revera %q",
				c.Pattern, c.Subject, c.Replacement, out0, out1)
		}
	}
}

func TestDifferentialReplaceNoSubErrorPrecedence(t *testing.T) {
	re0, re1, ok := compileBoth(t, "a", revera.FlagNoSub)
	if !ok {
		t.Fatal("NoSub pattern failed to compile")
	}
	for _, replacement := range []string{"-", `\`, `\1`} {
		_, err0 := re0.ReplaceAll("a", replacement, -1, 0)
		_, err1 := revera.ReplaceAll(&re1, "a", replacement, -1, 0)
		if referenceCode(err0) != revera.ErrENoSub || err1.Code != revera.ErrENoSub {
			t.Fatalf("NoSub replacement %q: reference code=%d, revera code=%d",
				replacement, referenceCode(err0), err1.Code)
		}
	}
}

func TestDifferentialMatchIter(t *testing.T) {
	for _, pattern := range protocol.IterPatterns {
		if pattern == "" {
			continue
		}
		for _, subject := range protocol.IterSubjects {
			for _, limit := range []int{-1, 0, 1, 2} {
				re0, re1, ok := compileBoth(t, pattern, 0)
				if !ok {
					continue
				}
				var spans0 [][]reference.Match
				err0 := re0.MatchAll(subject, limit, 0, func(pmatch []reference.Match) bool {
					row := make([]reference.Match, len(pmatch))
					copy(row, pmatch)
					spans0 = append(spans0, row)
					return true
				})
				if err0 != nil {
					t.Fatalf("reference MatchAll(%q, %q): %v", pattern, subject, err0)
				}
				it, ierr := revera.MatchIterInit(&re1, limit)
				if ierr.Code != revera.ErrNone {
					t.Fatalf("revera MatchIterInit: code %d", ierr.Code)
				}
				pmatch := make([]revera.Match, revera.NumSub(&re1)+1)
				var spans1 [][]revera.Match
				for {
					got, nerr := revera.MatchIterNext(&re1, &it, subject, 0, pmatch)
					if nerr.Code != revera.ErrNone {
						t.Fatalf("revera MatchIterNext: code %d", nerr.Code)
					}
					if !got {
						break
					}
					row := make([]revera.Match, len(pmatch))
					copy(row, pmatch)
					spans1 = append(spans1, row)
				}
				if len(spans0) != len(spans1) {
					t.Fatalf("MatchAll(%q, %q, limit=%d): reference %d matches, revera %d",
						pattern, subject, limit, len(spans0), len(spans1))
				}
				for i := range spans0 {
					for k := range spans0[i] {
						if spans0[i][k].So != spans1[i][k].So ||
							spans0[i][k].Eo != spans1[i][k].Eo {
							t.Fatalf("MatchAll(%q, %q): row %d reference %v revera %v",
								pattern, subject, i, spans0[i], spans1[i])
						}
					}
				}
			}
		}
	}
}

func TestErrorText(t *testing.T) {
	if revera.ErrorText(revera.ErrNone) != "success" ||
		revera.ErrorText(revera.ErrESpace) != "capacity limit reached" ||
		revera.ErrorText(99) != "unknown error" {
		t.Fatal("unexpected error text")
	}
}

func TestContractSmoke(t *testing.T) {
	re, err := revera.New("(a|ab)(c|bcd)(d*)")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	c := re.Contract(1 << 12)
	if !c.HasSolver {
		t.Fatal("expected a solver contract")
	}
	if c.HeapBytes() <= 0 || c.StackBytes() <= 0 || c.Steps() <= 0 {
		t.Fatalf("contract has nonpositive figures: %+v", c)
	}
	if re.Contract(16).Steps() > c.Steps() {
		t.Fatal("contract steps must grow with the input bound")
	}
}
