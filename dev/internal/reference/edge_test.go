package reference

import (
	"strings"
	"testing"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

func TestPmatchTruncation(t *testing.T) {
	re := compileOK(t, "(a)(b)(c)", 0)
	pmatch := make([]Match, 2)
	ok, err := re.Exec("abc", pmatch, 0)
	if err != nil || !ok {
		t.Fatalf("Exec: %v %v", ok, err)
	}
	if pmatch[0] != (Match{0, 3}) || pmatch[1] != (Match{0, 1}) {
		t.Fatalf("truncated pmatch = %v", pmatch)
	}
}

func TestHugeExpansionFallback(t *testing.T) {
	// 20 bytes of pattern give 255^3 characters of minimum match length.
	// Section 3.4 requires compilation to succeed.
	// Execution answers correctly for every subject shorter than the minimum length.
	re, err := Compile("((a{255}){255}){255}", locale.POSIX(), 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	pmatch := make([]Match, re.NumSub()+1)
	ok, err := re.Exec(strings.Repeat("a", 100000), pmatch, 0)
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if ok {
		t.Fatal("matched a subject shorter than the minimum length")
	}

	// A moderate expansion still runs normally.
	re = compileOK(t, "(a{100}){100}", 0)
	subject := strings.Repeat("a", 10000)
	ok, err = re.Exec(subject, pmatch[:1], 0)
	if err != nil || !ok {
		t.Fatalf("(a{100}){100}: %v %v", ok, err)
	}
	if pmatch[0] != (Match{0, 10000}) {
		t.Fatalf("(a{100}){100} span = %v", pmatch[0])
	}
	ok, err = re.Exec(strings.Repeat("a", 9999), pmatch[:1], 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("(a{100}){100} matched 9999 characters")
	}
}

func TestEquivalenceStarNoPanic(t *testing.T) {
	re, err := Compile("([[=a=]]*)", locale.POSIX(), ICase)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	pmatch := make([]Match, 2)
	ok, err := re.Exec("aaaaaaaaa", pmatch, 0)
	if err != nil || !ok {
		t.Fatalf("Exec: %v %v", ok, err)
	}
	if pmatch[0] != (Match{0, 9}) || pmatch[1] != (Match{0, 9}) {
		t.Fatalf("pmatch = %v", pmatch)
	}
}

func TestHugeExpansionMultiElementFallback(t *testing.T) {
	cs, ok := locale.Open("cs", "")
	if !ok {
		t.Fatal("cs locale missing")
	}
	// The minimum match length counts two characters per [[.ch.]].
	// A shorter subject therefore answers no-match instead of ESpace.
	re, err := Compile("(([[.ch.]]{255}){255}){255}", cs, 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	matched, err := re.Exec(strings.Repeat("ch", 500_000), nil, 0)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if matched {
		t.Fatal("matched below the minimum length")
	}
}

func TestHugeNullableExistence(t *testing.T) {
	// A nullable oversized pattern still answers existence questions.
	re, err := Compile("((a{0,255}){255}){255}", locale.POSIX(), NoSub)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	matched, err := re.Exec("anything", nil, 0)
	if err != nil || !matched {
		t.Fatalf("NoSub existence: %v %v", matched, err)
	}

	// Asking for offsets would need the oversized program.
	re, err = Compile("((a{0,255}){255}){255}", locale.POSIX(), 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	pmatch := make([]Match, 1)
	_, err = re.Exec("anything", pmatch, 0)
	if err == nil || err.(*Error).Code != ESpace {
		t.Fatalf("offset request: err = %v, want ESpace", err)
	}
}

func TestPrunedBranchStillMatches(t *testing.T) {
	// One oversized branch must not break the reachable branch.
	// The program prunes the huge subtree.
	// It stays exact for any subject shorter than the minimum match length of that subtree.
	re, err := Compile("(x|((a{255}){255}){255})", locale.POSIX(), 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	pmatch := make([]Match, re.NumSub()+1)
	matched, err := re.Exec("zzxq", pmatch, 0)
	if err != nil || !matched {
		t.Fatalf("Exec: %v %v", matched, err)
	}
	if pmatch[0] != (Match{2, 3}) || pmatch[1] != (Match{2, 3}) {
		t.Fatalf("pmatch = %v", pmatch)
	}
	if matched, err = re.Exec("zzzz", pmatch, 0); err != nil || matched {
		t.Fatalf("no-match case: %v %v", matched, err)
	}
}

func TestPrunedBranchExistencePastFailMin(t *testing.T) {
	// The huge branch prunes with a minimum match length of two.
	// A subject at least that long may reach the pruned subtree.
	// A match through the surviving branch still proves existence.
	re, err := Compile("x|(a((b{0,255}){255}){255}){2}", locale.POSIX(), 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if re.prog == nil || re.prog.failMin != 2 {
		t.Fatalf("expected a pruned program with failMin 2, got %+v", re.prog)
	}
	matched, err := re.Exec("xx", nil, 0)
	if err != nil || !matched {
		t.Fatalf("existence: %v %v", matched, err)
	}

	// A miss proves nothing, because the pruned subtree could have matched.
	if _, err = re.Exec("zz", nil, 0); err == nil ||
		err.(*Error).Code != ESpace {
		t.Fatalf("miss past failMin: err = %v, want ESpace", err)
	}

	// Offsets would need the full program.
	// The spans that the pruned program selects could be wrong.
	pmatch := make([]Match, 1)
	if _, err = re.Exec("xx", pmatch, 0); err == nil ||
		err.(*Error).Code != ESpace {
		t.Fatalf("offset request past failMin: err = %v, want ESpace", err)
	}

	// Below the minimum length of the pruned subtree, the program is exact.
	matched, err = re.Exec("x", pmatch, 0)
	if err != nil || !matched || pmatch[0] != (Match{0, 1}) {
		t.Fatalf("exact case: %v %v %v", matched, err, pmatch)
	}
}

func TestHugeExpansionEquivMinLength(t *testing.T) {
	cs, ok := locale.Open("cs", "")
	if !ok {
		t.Fatal("cs locale missing")
	}
	// No single character is primary equal to Czech ch.
	// The minimum match length therefore counts two characters per [[=ch=]].
	// A subject below that bound answers no-match instead of ESpace.
	re, err := Compile("(([[=ch=]]{255}){255}){255}", cs, 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	const wantMin = 2 * 255 * 255 * 255
	if re.prog == nil || re.prog.failMin != wantMin {
		t.Fatalf("expected a pruned program with failMin %d, got %+v",
			wantMin, re.prog)
	}
	matched, err := re.Exec(strings.Repeat("ch", 10_000_000), nil, 0)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if matched {
		t.Fatal("matched below the minimum length")
	}
}

func TestCaptureLongAmbiguousSubject(t *testing.T) {
	// A plain ambiguous pattern with captures must handle long subjects.
	// The node length bounds clamp the split ranges.
	re := compileOK(t, "((a|aa)*)", 0)
	subject := strings.Repeat("a", 5000)
	pmatch := make([]Match, 3)
	matched, err := re.Exec(subject, pmatch, 0)
	if err != nil || !matched {
		t.Fatalf("Exec: %v %v", matched, err)
	}
	want := []Match{{0, 5000}, {0, 5000}, {4998, 5000}}
	for idx := range want {
		if pmatch[idx] != want[idx] {
			t.Fatalf("pmatch[%d] = %v, want %v", idx, pmatch[idx], want[idx])
		}
	}
}

func TestICaseKelvinSign(t *testing.T) {
	en, ok := locale.Open("en", "")
	if !ok {
		t.Fatal("en locale missing")
	}
	kelvin := "\u212a"

	// The closure of the literal k is {k, K}.
	// The Kelvin sign maps to k, but nothing maps to the sign, so it stays outside.
	re, err := Compile("k", en, ICase)
	if err != nil {
		t.Fatal(err)
	}
	if matched, _ := re.Exec(kelvin, nil, 0); matched {
		t.Fatal("literal k matched the Kelvin sign")
	}

	// The closure of the literal Kelvin sign includes its lowercase k.
	re, err = Compile(kelvin, en, ICase)
	if err != nil {
		t.Fatal(err)
	}
	if matched, _ := re.Exec("k", nil, 0); !matched {
		t.Fatal("literal Kelvin sign missed k")
	}

	// Bracket membership uses preimages.
	// k has the sources K and the Kelvin sign, so [K] accepts k, and [k] rejects the sign.
	re, err = Compile("["+kelvin+"]", en, ICase)
	if err != nil {
		t.Fatal(err)
	}
	if matched, _ := re.Exec("k", nil, 0); !matched {
		t.Fatal("[Kelvin] missed k")
	}
	re, err = Compile("[k]", en, ICase)
	if err != nil {
		t.Fatal(err)
	}
	if matched, _ := re.Exec(kelvin, nil, 0); matched {
		t.Fatal("[k] matched the Kelvin sign")
	}
}

func TestCounterSlotsBeyond65535(t *testing.T) {
	// Overflow counter slots must not wrap at 65,536.
	// The dead block holds 65,535 shortest-preferring repetitions inside a {0} group.
	// That pushes the last live repetition past the uint16 range.
	dead := strings.Repeat("a{0}?", 65_535)
	pattern := "[ab]*?(" + dead + "){0}([ab]*?c|b)"
	re, err := Compile(pattern, locale.POSIX(), 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	pmatch := make([]Match, 1)
	matched, err := re.Exec("abc", pmatch, 0)
	if err != nil || !matched {
		t.Fatalf("Exec: %v %v", matched, err)
	}
	if pmatch[0] != (Match{0, 3}) {
		t.Fatalf("span = %v, want [0,3)", pmatch[0])
	}
}

func TestManyMinimalRepetitions(t *testing.T) {
	// More shortest-preferring repetitions than one mask word holds.
	pattern := strings.Repeat("a*?", 70) + "b"
	re, err := Compile(pattern, locale.POSIX(), 0)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	pmatch := make([]Match, 1)
	ok, err := re.Exec("aaab", pmatch, 0)
	if err != nil || !ok {
		t.Fatalf("Exec: %v %v", ok, err)
	}
	if pmatch[0] != (Match{0, 4}) {
		t.Fatalf("span = %v", pmatch[0])
	}
	got := make([]Match, 1)
	want := make([]Match, 1)
	okGot, _ := re.Exec("aab", got, 0)
	okWant, _ := re.oracleFullExec("aab", want, 0)
	if okGot != okWant || got[0] != want[0] {
		t.Fatalf("engine %v %v, oracle %v %v", okGot, got, okWant, want)
	}
}

func TestAnchorOnlyPatterns(t *testing.T) {
	runMatchCases(t, []matchCase{
		{pattern: "^$", subject: "", want: []Match{{0, 0}}},
		{pattern: "^$", subject: "a", want: nil},
		{pattern: "^$", subject: "a\n\nb", cflags: Newline,
			want: []Match{{2, 2}}},
		{pattern: "^^a$$", subject: "a", want: []Match{{0, 1}}},
	})
}

func TestNULInBracket(t *testing.T) {
	re := compileOK(t, "[\x00a]", 0)
	pmatch := make([]Match, 1)
	ok, err := re.Exec("\x00", pmatch, 0)
	if err != nil || !ok {
		t.Fatalf("bracket NUL: %v %v", ok, err)
	}
	if pmatch[0] != (Match{0, 1}) {
		t.Fatalf("bracket NUL span = %v", pmatch[0])
	}
}

func TestICaseCollatingElement(t *testing.T) {
	cs, ok := locale.Open("cs", "")
	if !ok {
		t.Fatal("cs locale missing")
	}
	re, err := Compile("[[.ch.]]", cs, ICase)
	if err != nil {
		t.Fatalf("cs icase [[.ch.]]: %v", err)
	}
	pmatch := make([]Match, 1)
	for _, subject := range []string{"ch", "Ch", "cH", "CH"} {
		matched, err := re.Exec(subject, pmatch, 0)
		if err != nil || !matched {
			t.Fatalf("icase [[.ch.]] on %q: %v %v", subject, matched, err)
		}
		if pmatch[0] != (Match{0, 2}) {
			t.Fatalf("icase [[.ch.]] on %q consumed %v", subject, pmatch[0])
		}
	}
}

func TestMoreErrorSpellings(t *testing.T) {
	cases := []struct {
		pattern string
		code    Code
	}{
		{"(|a)", BadPat},
		{"(a|)", BadPat},
		{"a|*", BadRpt},
		{"(*a)", BadRpt},
		{"a{1,2,3}", BadBR},
		{"[a-\\]", ERange}, // backslash is ordinary, and \ < a gives an empty range
		{"[[.nonsuch.]]", ECollate},
		{"[[:upper:]", EBrack},
		{"x[[:alpha]]", EBrack},
	}
	for _, c := range cases {
		_, err := Compile(c.pattern, locale.POSIX(), 0)
		if err == nil {
			t.Errorf("Compile(%q) succeeded, want %v", c.pattern, c.code)
			continue
		}
		if got := err.(*Error).Code; got != c.code {
			t.Errorf("Compile(%q) = %v, want %v", c.pattern, got, c.code)
		}
	}
}

func TestScanFilterAnchors(t *testing.T) {
	long := strings.Repeat("z", 5000)
	runMatchCases(t, []matchCase{
		{pattern: "^abc", subject: long + "abc", want: nil},
		{pattern: "^abc", subject: "abc" + long,
			want: []Match{{0, 3}}},
		{pattern: "^abc", subject: long + "\nabc", cflags: Newline,
			want: []Match{{5001, 5004}}},
		{pattern: "abc$", subject: long + "abc",
			want: []Match{{5000, 5003}}},
		{pattern: "abc$", subject: "abc" + long, want: nil},
		{pattern: "abc$", subject: "abc\n" + long, cflags: Newline,
			want: []Match{{0, 3}}},
		{pattern: "needle", subject: long + "needle" + long,
			want: []Match{{5000, 5006}}},
		{pattern: "needle", subject: long, want: nil},
		{pattern: "néedle", subject: long + "néedle",
			want: []Match{{5000, 5007}}},
	})
}

func TestManyGroups(t *testing.T) {
	// Group numbering has no nine-group limit.
	pattern := ""
	subject := ""
	for c := 'a'; c <= 'l'; c++ {
		pattern += "(" + string(c) + ")"
		subject += string(c)
	}
	re := compileOK(t, pattern, 0)
	if re.NumSub() != 12 {
		t.Fatalf("NumSub = %d", re.NumSub())
	}
	caps := execCaps(t, re, subject, 0)
	if caps == nil {
		t.Fatal("no match")
	}
	for idx := 1; idx <= 12; idx++ {
		if caps[idx] != (Match{idx - 1, idx}) {
			t.Fatalf("group %d = %v", idx, caps[idx])
		}
	}
}

func TestErrorMessage(t *testing.T) {
	_, err := Compile("(a", locale.POSIX(), 0)
	if err == nil {
		t.Fatal("expected error")
	}
	message := err.Error()
	if !strings.Contains(message, "parenthesis") {
		t.Fatalf("message = %q", message)
	}
}

func TestNonPOSIXLocaleClasses(t *testing.T) {
	fr, ok := locale.Open("fr", "")
	if !ok {
		t.Fatal("fr locale missing")
	}
	re, err := Compile("[[:alpha:]]+", fr, 0)
	if err != nil {
		t.Fatal(err)
	}
	pmatch := make([]Match, 1)
	matched, err := re.Exec("héhé", pmatch, 0)
	if err != nil || !matched {
		t.Fatalf("fr alpha: %v %v", matched, err)
	}
	if pmatch[0] != (Match{0, 6}) {
		t.Fatalf("fr alpha span = %v", pmatch[0])
	}

	// The POSIX locale must not treat é as alphabetic.
	re = compileOK(t, "[[:alpha:]]+", 0)
	matched, err = re.Exec("héhé", pmatch, 0)
	if err != nil || !matched {
		t.Fatalf("posix alpha: %v %v", matched, err)
	}
	if pmatch[0] != (Match{0, 1}) {
		t.Fatalf("posix alpha span = %v", pmatch[0])
	}
}

func TestICaseNonPOSIXLocale(t *testing.T) {
	tr, ok := locale.Open("tr", "")
	if !ok {
		t.Fatal("tr locale missing")
	}
	re, err := Compile("i", tr, ICase)
	if err != nil {
		t.Fatal(err)
	}
	pmatch := make([]Match, 1)
	// In Turkish the uppercase counterpart of i is İ, not I.
	matched, err := re.Exec("İ", pmatch, 0)
	if err != nil || !matched {
		t.Fatalf("tr icase i on İ: %v %v", matched, err)
	}
	matched, err = re.Exec("I", pmatch, 0)
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Fatal("tr icase i matched I")
	}
}
