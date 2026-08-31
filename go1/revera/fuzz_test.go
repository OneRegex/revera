package revera

import (
	"bytes"
	"testing"

	g0 "revera"
	g0loc "revera/locale"
)

// fuzzLocales resolves the locale of an input for both engines.
func fuzzLocales(t *testing.T, base *Locale, name string) (g0loc.Locale, Locale, bool) {
	t.Helper()
	loc1, ok1 := LocaleByName(base, name)
	loc0, ok0 := g0loc.POSIX(), true
	if name != "" {
		loc0, ok0 = g0loc.Open(name, "")
	}
	if ok0 != ok1 {
		t.Fatalf("locale %q: go0 ok=%v, go1 ok=%v", name, ok0, ok1)
	}
	return loc0, loc1, ok1
}

// fuzzCompare runs one decoded input through go0 and go1 and requires the same answers.
// go1 runs first and its capacity errors end the comparison, because go0 has no early arena cap.
func fuzzCompare(t *testing.T, base *Locale, in FuzzInput) {
	t.Helper()
	loc0, loc1, ok := fuzzLocales(t, base, in.Locale)
	if !ok {
		return
	}
	re1, err1 := Compile(in.Pattern, loc1, in.Flags)
	re0, err0 := g0.Compile(in.Pattern, loc0, g0.CompileFlags(in.Flags))
	if (err0 == nil) != (err1.Code == ErrNone) {
		t.Fatalf("Compile(%q, %d): go0 err=%v, go1 code=%d", in.Pattern, in.Flags, err0, err1.Code)
	}
	if err0 != nil {
		if g0Code(err0) != err1.Code || err0.(*g0.Error).Pos != err1.Pos {
			t.Fatalf("Compile(%q, %d): go0 (%d,%d), go1 (%d,%d)",
				in.Pattern, in.Flags, g0Code(err0), err0.(*g0.Error).Pos, err1.Code, err1.Pos)
		}
		return
	}
	if re0.NumSub() != NumSub(&re1) {
		t.Fatalf("Compile(%q): NumSub go0=%d go1=%d", in.Pattern, re0.NumSub(), NumSub(&re1))
	}
	got1 := make([]Match, NumSub(&re1)+1)
	ok1, xerr1 := Exec(&re1, in.Subject, got1, in.EFlags)
	if xerr1.Code == ErrESpace {
		return
	}
	got0 := make([]g0.Match, re0.NumSub()+1)
	ok0, xerr0 := re0.Exec(in.Subject, got0, g0.ExecFlags(in.EFlags))
	if g0Code(xerr0) != xerr1.Code {
		t.Fatalf("%q on %q eflags=%d: go0 code=%d, go1 code=%d",
			in.Pattern, in.Subject, in.EFlags, g0Code(xerr0), xerr1.Code)
	}
	if xerr1.Code == ErrNone {
		if ok0 != ok1 {
			t.Fatalf("%q on %q eflags=%d: go0 matched=%v, go1 matched=%v",
				in.Pattern, in.Subject, in.EFlags, ok0, ok1)
		}
		for idx := range got0 {
			if ok0 && (got0[idx].So != got1[idx].So || got0[idx].Eo != got1[idx].Eo) {
				t.Fatalf("%q on %q eflags=%d: go0 %v, go1 %v",
					in.Pattern, in.Subject, in.EFlags, got0, got1)
			}
		}
	}
	out1, rerr1 := ReplaceAll(&re1, in.Subject, in.Replacement, -1, in.EFlags)
	if rerr1.Code == ErrESpace {
		return
	}
	out0, rerr0 := re0.ReplaceAll(in.Subject, in.Replacement, -1, g0.ExecFlags(in.EFlags))
	if g0Code(rerr0) != rerr1.Code {
		t.Fatalf("ReplaceAll(%q, %q, %q): go0 code=%d, go1 code=%d",
			in.Pattern, in.Subject, in.Replacement, g0Code(rerr0), rerr1.Code)
	}
	if rerr1.Code == ErrNone && out0 != out1 {
		t.Fatalf("ReplaceAll(%q, %q, %q): go0 %q, go1 %q",
			in.Pattern, in.Subject, in.Replacement, out0, out1)
	}
}

// FuzzEngine is the Go fuzz entry point.
// It runs the shared crash-only procedure on every input, then compares go1 against go0 on the inputs small enough for go0.
func FuzzEngine(f *testing.F) {
	for _, seed := range FuzzSeeds() {
		f.Add(seed)
	}
	base := MustEmbeddedLocale()
	f.Fuzz(func(t *testing.T, data []byte) {
		FuzzRun(&base, data)
		in, ok := DecodeFuzzInput(data)
		if ok && len(in.Pattern) <= 64 && len(in.Subject) <= 256 {
			fuzzCompare(t, &base, in)
		}
	})
}

func TestFuzzInputRoundTrip(t *testing.T) {
	in := FuzzInput{Flags: FlagICase | FlagMinimal, EFlags: 2, Locale: "tr", Pattern: "(a|b)*", Replacement: `\1`, Subject: "abab"}
	got, ok := DecodeFuzzInput(EncodeFuzzInput(in))
	if !ok || got != in {
		t.Fatalf("round trip changed the input: %+v -> %+v", in, got)
	}
	if _, ok := DecodeFuzzInput([]byte{1, 2}); ok {
		t.Fatal("a two-byte input must decode to nothing")
	}
	short, ok := DecodeFuzzInput([]byte{0, 0, 9, 'a', 'b'})
	if !ok || short.Pattern != "ab" || short.Subject != "" {
		t.Fatalf("a cut pattern must keep what is there: %+v", short)
	}
}

func TestFuzzPackRoundTrip(t *testing.T) {
	seeds := FuzzSeeds()
	var buf bytes.Buffer
	if err := WriteFuzzPack(&buf, seeds); err != nil {
		t.Fatal(err)
	}
	back, err := ReadFuzzPack(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(seeds) {
		t.Fatalf("pack holds %d records, want %d", len(back), len(seeds))
	}
	for i := range seeds {
		if !bytes.Equal(seeds[i], back[i]) {
			t.Fatalf("record %d changed", i)
		}
	}
	if _, err := ReadFuzzPack(bytes.NewReader([]byte{5, 0, 0, 0, 'a'})); err == nil {
		t.Fatal("a truncated record must fail")
	}
}
