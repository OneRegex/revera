package revera

// Locale differential: every observable locale operation must match the go0 locale package on the same data.

import (
	"math/rand"
	"slices"
	"testing"
	"unicode/utf8"

	g0loc "revera/locale"
)

var localeSamples = []struct {
	name  string
	ctype string
}{
	{"C", ""},
	{"posix", ""},
	{"en", ""},
	{"cs", ""},
	{"tr", ""},
	{"de", "phonebook"},
	{"de@collation=phonebook", ""},
	{"da", ""},
	{"hu", ""},
	{"ja", ""},
	{"fr-FR.UTF-8", ""},
}

func sampleRunes() []int32 {
	out := make([]int32, 0, 3000)
	for r := int32(0); r < 0x300; r++ {
		out = append(out, r)
	}
	extra := []int32{0x130, 0x131, 0x17f, 0x1c4, 0x1c5, 0x1c6, 0x212a,
		0x212b, 0xdf, 0x1e9e, 0x3a3, 0x3c2, 0x3c3, 0x10400, 0x10428,
		0x1f600, 0x10ffff, -1, 0xd800}
	out = append(out, extra...)
	rng := rand.New(rand.NewSource(11))
	for range 1000 {
		out = append(out, int32(rng.Intn(0x110000)))
	}
	return out
}

func TestLocaleOpenDifferential(t *testing.T) {
	names := []string{"C", "POSIX", "c.UTF-8", "en", "EN_us", "xx-not-there",
		"de@collation=phonebook", "de@bad", "cs.utf8", "cs.UTF8", "cs.u-t-f-8",
		"cs.UTF--8", "cs.UTF-8-", "cs.latin2", "",
		"tr", "sv", "de@collation=dictionary"}
	for _, name := range names {
		_, ok0 := g0loc.Open(name, "")
		_, err1 := OpenLocale(name, "")
		ok1 := err1 == nil
		if ok0 != ok1 {
			t.Fatalf("OpenLocale(%q): go0 ok=%v, go1 ok=%v", name, ok0, ok1)
		}
		if (name == "cs.u-t-f-8" || name == "cs.UTF--8" || name == "cs.UTF-8-") && ok1 {
			t.Fatalf("OpenLocale(%q) accepted a malformed UTF-8 suffix", name)
		}
	}
	if g0loc.Count() != LocaleCountEmbedded(t) {
		t.Fatalf("locale count mismatch")
	}
}

// LocaleCountEmbedded opens one real locale to reach the loaded section table, then counts.
func LocaleCountEmbedded(t *testing.T) int {
	l, err := OpenLocale("en", "")
	if err != nil {
		t.Fatal("en locale missing")
	}
	return LocaleCount(&l)
}

func TestLocaleOpsDifferential(t *testing.T) {
	runes := sampleRunes()
	for _, sel := range localeSamples {
		l0, ok0 := g0loc.Open(sel.name, sel.ctype)
		l1, err1 := OpenLocale(sel.name, sel.ctype)
		ok1 := err1 == nil
		if ok0 != ok1 {
			t.Fatalf("OpenLocale(%q,%q): go0 ok=%v, go1 ok=%v", sel.name, sel.ctype, ok0, ok1)
		}
		if !ok0 {
			continue
		}
		if g0loc.MaxElementLength() < 1 {
			t.Fatal("go0 max element length")
		}
		for _, r := range runes {
			if m0, m1 := l0.ClassMask(r), localeClassMask(&l1, r); m0 != m1 {
				t.Fatalf("%s: ClassMask(%#x): go0 %04x, go1 %04x", sel.name, r, m0, m1)
			}
			if u0, u1 := l0.ToUpper(r), localeToUpper(&l1, r); u0 != u1 {
				t.Fatalf("%s: ToUpper(%#x): go0 %#x, go1 %#x", sel.name, r, u0, u1)
			}
			if d0, d1 := l0.ToLower(r), localeToLower(&l1, r); d0 != d1 {
				t.Fatalf("%s: ToLower(%#x): go0 %#x, go1 %#x", sel.name, r, d0, d1)
			}
			pre0 := l0.AppendCasePreimages(nil, r)
			var buf preimageBuf
			localeCasePreimages(&l1, &buf, r)
			pre1 := make([]rune, 0, buf.n)
			for i := 0; i < buf.n; i++ {
				pre1 = append(pre1, buf.r[i])
			}
			slices.Sort(pre0)
			slices.Sort(pre1)
			if !slices.Equal(pre0, pre1) {
				t.Fatalf("%s: preimages(%#x): go0 %v, go1 %v", sel.name, r, pre0, pre1)
			}
		}
	}
}

func TestLocaleCollationDifferential(t *testing.T) {
	seqs := [][]int32{
		{'c', 'h'}, {'C', 'H'}, {'c'}, {'x', 'y'}, {'d', 'z', 's'},
		{'a', 'e'}, {0x153}, {'s', 's'}, {'l', 'l'}, {'i', 'j'},
		{'t', 'h'}, {'n', 'g'}, {'a'}, {},
	}
	for _, sel := range localeSamples {
		l0, ok0 := g0loc.Open(sel.name, sel.ctype)
		if !ok0 {
			continue
		}
		l1, _ := OpenLocale(sel.name, sel.ctype)
		for _, seq := range seqs {
			if c0, c1 := l0.IsCollatingElement(seq), localeIsCollatingElement(&l1, seq); c0 != c1 {
				t.Fatalf("%s: IsCollatingElement(%v): go0 %v, go1 %v", sel.name, seq, c0, c1)
			}
			if p0, p1 := l0.CollatingPrefix(seq), LocaleCollatingPrefix(&l1, seq); p0 != p1 {
				t.Fatalf("%s: CollatingPrefix(%v): go0 %d, go1 %d", sel.name, seq, p0, p1)
			}
			if m0, m1 := l0.MinEquivLength(seq), localeMinEquivLength(&l1, seq); m0 != m1 {
				t.Fatalf("%s: MinEquivLength(%v): go0 %d, go1 %d", sel.name, seq, m0, m1)
			}
			for _, other := range seqs {
				e0 := l0.PrimaryEqual(seq, other)
				e1 := localePrimaryEqual(&l1, seq, other)
				if e0 != e1 {
					t.Fatalf("%s: PrimaryEqual(%v,%v): go0 %v, go1 %v",
						sel.name, seq, other, e0, e1)
				}
			}
		}
	}
}

// TestLocaleOpenMalformedData corrupts the blob.
// It requires a clean rejection or a working locale, never a panic.
// It poisons every u32 field of the locale-name offset table, and random single-byte flips cover the rest of the layout.
func TestLocaleOpenMalformedData(t *testing.T) {
	blob := EmbeddedLocaleData()
	probe := func(data string) {
		l, ok := LocaleOpen(data, "cs", "")
		if !ok {
			return
		}
		// An accepted blob must answer lookups without a panic.
		localeClassMask(&l, 'a')
		localeToUpper(&l, 'i')
		localeIsCollatingElement(&l, []int32{'c', 'h'})
		localeMinEquivLength(&l, []int32{'c', 'h'})
		LocaleName(&l, 0)
	}
	var loaded Locale
	if !localeLoad(&loaded, blob) {
		t.Fatal("embedded blob must load")
	}
	poison := []uint8{0xff, 0xff, 0xff, 0xff}
	sec := loaded.sec[secLocaleNameOffsets]
	for off := sec.Off; off+4 <= sec.End; off += 4 {
		raw := []uint8(blob)
		copy(raw[off:off+4], poison)
		probe(string(raw))
	}
	rng := rand.New(rand.NewSource(21))
	for range 300 {
		raw := []uint8(blob)
		at := rng.Intn(len(raw))
		raw[at] ^= uint8(1 + rng.Intn(255))
		probe(string(raw))
	}
	for range 100 {
		// Truncations exercise the section table parser.
		probe(blob[:rng.Intn(len(blob))])
	}
}

func TestUTF8Differential(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	for range 20000 {
		length := rng.Intn(6)
		raw := make([]byte, length)
		rng.Read(raw)
		s := string(raw)
		at := 0
		for at < len(s) {
			r1, size1 := decodeRuneAt(s, at)
			r0, size0 := g0DecodeRune(s[at:])
			if r0 != r1 || size0 != size1 {
				t.Fatalf("decode %q at %d: go0 (%#x,%d), go1 (%#x,%d)",
					s, at, r0, size0, r1, size1)
			}
			at += size1
		}
	}
}

// g0DecodeRune mirrors the decodeRune of go0 with the stdlib decoder.
// An invalid sequence maps to the sentinel, and an encoded U+FFFD stays a three-byte character.
func g0DecodeRune(s string) (rune, int) {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size <= 1 {
		return -1, 1
	}
	return r, size
}
