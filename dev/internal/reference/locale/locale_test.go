package locale

import (
	"slices"
	"testing"
)

func TestOpenPOSIXAliases(t *testing.T) {
	for _, name := range []string{"C", "c", "POSIX", "posix", "C.UTF-8", "POSIX.utf8"} {
		l, ok := Open(name, "")
		if !ok || !l.IsPOSIX() {
			t.Fatalf("Open(%q) = %v, %v", name, l, ok)
		}
	}
	if _, ok := Open("C", "phonebook"); ok {
		t.Fatal("POSIX locale accepted a collation type")
	}
	if l, ok := Open("POSIX", "standard"); !ok || !l.IsPOSIX() {
		t.Fatal("POSIX locale rejected the standard collation type")
	}
}

func TestOpenNames(t *testing.T) {
	for _, name := range []string{"en", "en-US", "en_US", "en_US.UTF-8", "en_US.UTF8", "fr-FR", "cs", "tr"} {
		l, ok := Open(name, "")
		if !ok || l.IsPOSIX() {
			t.Fatalf("Open(%q) = %v, %v", name, l, ok)
		}
	}
	for _, name := range []string{"", "zz-ZZ", "en_US.ISO8859-1", "en.u-t-f-8", "en.UTF--8", "en.UTF-8-", "en@x=y", "en@collation="} {
		if _, ok := Open(name, ""); ok {
			t.Fatalf("Open(%q) unexpectedly succeeded", name)
		}
	}
}

func TestOpenCollationTypes(t *testing.T) {
	direct, ok := Open("de", "phonebook")
	if !ok {
		t.Fatal("de phonebook rejected")
	}
	short, ok := Open("de", "phonebk")
	if !ok || short != direct {
		t.Fatal("phonebk alias mismatch")
	}
	modifier, ok := Open("de@collation=phonebk", "")
	if !ok || modifier != direct {
		t.Fatal("modifier selection mismatch")
	}
	if _, ok := Open("de@collation=phonebk", "standard"); ok {
		t.Fatal("modifier plus type argument accepted")
	}
	if _, ok := Open("de", "nosuchtype"); ok {
		t.Fatal("unknown type accepted")
	}
}

func TestPOSIXClasses(t *testing.T) {
	p := POSIX()
	cases := []struct {
		class Class
		r     rune
		want  bool
	}{
		{Digit, '7', true},
		{Digit, 'a', false},
		{Digit, '٧', false},
		{Alpha, 'a', true},
		{Alpha, 'Z', true},
		{Alpha, 'é', false},
		{Blank, ' ', true},
		{Blank, '\t', true},
		{Blank, '\n', false},
		{Space, '\n', true},
		{Space, '\v', true},
		{Cntrl, 0x00, true},
		{Cntrl, 0x7f, true},
		{Punct, '!', true},
		{Punct, 'a', false},
		{Graph, ' ', false},
		{Print, ' ', true},
		{Xdigit, 'F', true},
		{Xdigit, 'g', false},
		{Upper, 'A', true},
		{Lower, 'A', false},
	}
	for _, c := range cases {
		if got := p.IsClass(c.class, c.r); got != c.want {
			t.Errorf("POSIX IsClass(%d, %q) = %v, want %v", c.class, c.r, got, c.want)
		}
	}
}

func TestCLDRClasses(t *testing.T) {
	en, _ := Open("en", "")
	cases := []struct {
		class Class
		r     rune
		want  bool
	}{
		{Alpha, 'é', true},
		{Alpha, 'Ж', true},
		{Alpha, '7', false},
		{Digit, '7', true},
		{Digit, '٧', false},
		{Xdigit, 'F', true},
		{Xdigit, 'G', false},
		{Upper, 'É', true},
		{Lower, 'é', true},
		{Space, 0x2028, true},
		{Cntrl, 0x9f, true},
	}
	for _, c := range cases {
		if got := en.IsClass(c.class, c.r); got != c.want {
			t.Errorf("en IsClass(%d, %#x) = %v, want %v", c.class, c.r, got, c.want)
		}
	}
}

func TestCase(t *testing.T) {
	p := POSIX()
	if p.ToUpper('a') != 'A' || p.ToLower('A') != 'a' {
		t.Fatal("POSIX ASCII case broken")
	}
	if p.ToUpper('é') != 'é' {
		t.Fatal("POSIX must not map non-ASCII case")
	}
	en, _ := Open("en", "")
	if en.ToUpper('é') != 'É' || en.ToLower('É') != 'é' {
		t.Fatal("en case mapping broken")
	}
	if en.ToUpper('i') != 'I' {
		t.Fatal("en dotted i mapping broken")
	}
	tr, _ := Open("tr", "")
	if tr.ToUpper('i') != 'İ' {
		t.Fatalf("tr ToUpper(i) = %#x", tr.ToUpper('i'))
	}
	if tr.ToLower('I') != 'ı' {
		t.Fatalf("tr ToLower(I) = %#x", tr.ToLower('I'))
	}
	if tr.ToLower('İ') != 'i' {
		t.Fatalf("tr ToLower(İ) = %#x", tr.ToLower('İ'))
	}
}

func TestCasePreimages(t *testing.T) {
	p := POSIX()
	if got := p.AppendCasePreimages(nil, 'A'); len(got) != 1 || got[0] != 'a' {
		t.Fatalf("POSIX preimages of A = %v", got)
	}
	if got := p.AppendCasePreimages(nil, '!'); len(got) != 0 {
		t.Fatalf("POSIX preimages of ! = %v", got)
	}
	en, _ := Open("en", "")
	got := en.AppendCasePreimages(nil, 'k')
	hasLatin, hasKelvin := false, false
	for _, r := range got {
		if r == 'K' {
			hasLatin = true
		}
		if r == 0x212a {
			hasKelvin = true
		}
	}
	if !hasLatin || !hasKelvin {
		t.Fatalf("en preimages of k = %v, want K and Kelvin sign", got)
	}
	tr, _ := Open("tr", "")
	got = tr.AppendCasePreimages(nil, 'i')
	for _, r := range got {
		if r == 'I' {
			t.Fatalf("tr preimages of i = %v, must not contain I", got)
		}
	}
	hasDotted := false
	for _, r := range got {
		if r == 'İ' {
			hasDotted = true
		}
	}
	if !hasDotted {
		t.Fatalf("tr preimages of i = %v, want İ", got)
	}
}

// TestCasePreimageRoundTrip checks the inverse case tables against the forward mappings, over every scalar.
// It checks both soundness and completeness.
func TestCasePreimageRoundTrip(t *testing.T) {
	for _, name := range []string{"en", "tr"} {
		l, ok := Open(name, "")
		if !ok {
			t.Fatalf("%s locale missing", name)
		}
		var buffer [8]rune
		for r := rune(0); r <= 0x10ffff; r++ {
			if r >= 0xd800 && r <= 0xdfff {
				continue
			}
			for _, m := range l.AppendCasePreimages(buffer[:0], r) {
				if l.ToUpper(m) != r && l.ToLower(m) != r {
					t.Fatalf("%s: preimage %#x of %#x maps elsewhere", name, m, r)
				}
			}
			if upper := l.ToUpper(r); upper != r {
				found := slices.Contains(l.AppendCasePreimages(buffer[:0], upper), r)
				if !found {
					t.Fatalf("%s: %#x missing from preimages of its upper %#x",
						name, r, upper)
				}
			}
			if lower := l.ToLower(r); lower != r {
				found := slices.Contains(l.AppendCasePreimages(buffer[:0], lower), r)
				if !found {
					t.Fatalf("%s: %#x missing from preimages of its lower %#x",
						name, r, lower)
				}
			}
		}
	}
}

func TestCollatingElements(t *testing.T) {
	p := POSIX()
	if !p.IsCollatingElement([]rune{'a'}) {
		t.Fatal("single character must be a collating element")
	}
	if p.IsCollatingElement([]rune{'c', 'h'}) {
		t.Fatal("POSIX locale has no multi-character elements")
	}
	cs, _ := Open("cs", "")
	if !cs.IsCollatingElement([]rune{'c', 'h'}) {
		t.Fatal("cs must have the ch contraction")
	}
	if got := cs.CollatingPrefix([]rune("chleba")); got != 2 {
		t.Fatalf("cs CollatingPrefix(chleba) = %d", got)
	}
	if got := p.CollatingPrefix([]rune("chleba")); got != 1 {
		t.Fatalf("POSIX CollatingPrefix(chleba) = %d", got)
	}
}

func TestPrimaryEqual(t *testing.T) {
	p := POSIX()
	if !p.PrimaryEqual([]rune{'a'}, []rune{'a'}) {
		t.Fatal("identity equivalence broken")
	}
	if p.PrimaryEqual([]rune{'a'}, []rune{'A'}) {
		t.Fatal("POSIX a/A must not be primary equal")
	}
	en, _ := Open("en", "")
	if !en.PrimaryEqual([]rune{'a'}, []rune{'à'}) {
		t.Fatal("en a/à must be primary equal")
	}
	if en.PrimaryEqual([]rune{'a'}, []rune{'b'}) {
		t.Fatal("en a/b must not be primary equal")
	}
}

func TestMinEquivLength(t *testing.T) {
	cases := []struct {
		locale string
		seq    string
		want   int
	}{
		// No single character shares the primary weight of these digraphs, so their classes need two characters.
		{"cs", "ch", 2},
		{"hu", "cs", 2},
		{"cy", "ch", 2},
		// Danish aa is primary equal to the single character å.
		{"da", "aa", 1},
		{"cs", "a", 1},
		{"C", "a", 1},
	}
	for _, tc := range cases {
		l, ok := Open(tc.locale, "")
		if !ok {
			t.Fatalf("Open(%q) failed", tc.locale)
		}
		if got := l.MinEquivLength([]rune(tc.seq)); got != tc.want {
			t.Errorf("%s [[=%s=]]: MinEquivLength = %d, want %d",
				tc.locale, tc.seq, got, tc.want)
		}
	}
	da, _ := Open("da", "")
	if !da.PrimaryEqual([]rune{'å'}, []rune("aa")) {
		t.Fatal("da å/aa must be primary equal")
	}
}

func TestNames(t *testing.T) {
	if Count() != 1122 {
		t.Fatalf("Count() = %d", Count())
	}
	if Name(0) != "aa" {
		t.Fatalf("Name(0) = %q", Name(0))
	}
	if Name(Count()) != "" {
		t.Fatal("out-of-range Name must be empty")
	}
	for i := 1; i < Count(); i++ {
		if Name(i-1) >= Name(i) {
			t.Fatalf("names not sorted at %d: %q >= %q", i, Name(i-1), Name(i))
		}
	}
}

func TestSupportsRanges(t *testing.T) {
	if !POSIX().SupportsRanges() {
		t.Fatal("POSIX locale must support ranges")
	}
	en, _ := Open("en", "")
	if en.SupportsRanges() {
		t.Fatal("non-POSIX locales use the reject policy")
	}
}
