package differential

import (
	"testing"

	"github.com/oneregex/revera/dev/internal/protocol"
	"github.com/oneregex/revera/dev/internal/reference"
	"github.com/oneregex/revera/dev/internal/reference/locale"
	"github.com/oneregex/revera/go"
)

// fuzzLocales resolves the locale of an input for both engines.
func fuzzLocales(t *testing.T, base *revera.Locale, name string) (locale.Locale, revera.Locale, bool) {
	t.Helper()
	loc1, ok1 := protocol.LocaleByName(base, name)
	loc0, ok0 := locale.POSIX(), true
	if name != "" {
		loc0, ok0 = locale.Open(name, "")
	}
	if ok0 != ok1 {
		t.Fatalf("locale %q: reference ok=%v, revera ok=%v", name, ok0, ok1)
	}
	return loc0, loc1, ok1
}

// fuzzCompare runs one decoded input through reference and revera and requires the same answers.
// revera runs first and its capacity errors end the comparison, because reference has no early arena cap.
func fuzzCompare(t *testing.T, base *revera.Locale, in protocol.FuzzInput) {
	t.Helper()
	loc0, loc1, ok := fuzzLocales(t, base, in.Locale)
	if !ok {
		return
	}
	re1, err1 := revera.Compile(in.Pattern, loc1, in.Flags)
	re0, err0 := reference.Compile(in.Pattern, loc0, reference.CompileFlags(in.Flags))
	if (err0 == nil) != (err1.Code == revera.ErrNone) {
		t.Fatalf("Compile(%q, %d): reference err=%v, revera code=%d", in.Pattern, in.Flags, err0, err1.Code)
	}
	if err0 != nil {
		if referenceCode(err0) != err1.Code || err0.(*reference.Error).Pos != err1.Pos {
			t.Fatalf("Compile(%q, %d): reference (%d,%d), revera (%d,%d)",
				in.Pattern, in.Flags, referenceCode(err0), err0.(*reference.Error).Pos, err1.Code, err1.Pos)
		}
		return
	}
	if re0.NumSub() != revera.NumSub(&re1) {
		t.Fatalf("Compile(%q): NumSub reference=%d revera=%d", in.Pattern, re0.NumSub(), revera.NumSub(&re1))
	}
	got1 := make([]revera.Match, revera.NumSub(&re1)+1)
	ok1, xerr1 := revera.Exec(&re1, in.Subject, got1, in.EFlags)
	if xerr1.Code == revera.ErrESpace {
		return
	}
	got0 := make([]reference.Match, re0.NumSub()+1)
	ok0, xerr0 := re0.Exec(in.Subject, got0, reference.ExecFlags(in.EFlags))
	if referenceCode(xerr0) != xerr1.Code {
		t.Fatalf("%q on %q eflags=%d: reference code=%d, revera code=%d",
			in.Pattern, in.Subject, in.EFlags, referenceCode(xerr0), xerr1.Code)
	}
	if xerr1.Code == revera.ErrNone {
		if ok0 != ok1 {
			t.Fatalf("%q on %q eflags=%d: reference matched=%v, revera matched=%v",
				in.Pattern, in.Subject, in.EFlags, ok0, ok1)
		}
		for idx := range got0 {
			if ok0 && (got0[idx].So != got1[idx].So || got0[idx].Eo != got1[idx].Eo) {
				t.Fatalf("%q on %q eflags=%d: reference %v, revera %v",
					in.Pattern, in.Subject, in.EFlags, got0, got1)
			}
		}
	}
	out1, rerr1 := revera.ReplaceAll(&re1, in.Subject, in.Replacement, -1, in.EFlags)
	if rerr1.Code == revera.ErrESpace {
		return
	}
	out0, rerr0 := re0.ReplaceAll(in.Subject, in.Replacement, -1, reference.ExecFlags(in.EFlags))
	if referenceCode(rerr0) != rerr1.Code {
		t.Fatalf("ReplaceAll(%q, %q, %q): reference code=%d, revera code=%d",
			in.Pattern, in.Subject, in.Replacement, referenceCode(rerr0), rerr1.Code)
	}
	if rerr1.Code == revera.ErrNone && out0 != out1 {
		t.Fatalf("ReplaceAll(%q, %q, %q): reference %q, revera %q",
			in.Pattern, in.Subject, in.Replacement, out0, out1)
	}
}

// FuzzDifferential is the differential fuzz entry point.
// It runs the shared crash-only procedure on every input, then compares Revera against the reference engine on the inputs small enough for it.
// The crash-only procedure alone is FuzzEngine in the engine module.
func FuzzDifferential(f *testing.F) {
	for _, seed := range protocol.FuzzSeeds() {
		f.Add(seed)
	}
	base := protocol.MustEmbeddedLocale()
	f.Fuzz(func(t *testing.T, data []byte) {
		protocol.FuzzRun(&base, data)
		in, ok := protocol.DecodeFuzzInput(data)
		if ok && len(in.Pattern) <= 64 && len(in.Subject) <= 256 {
			fuzzCompare(t, &base, in)
		}
	})
}
