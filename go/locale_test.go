package revera

// Locale tests: every observable locale operation must match the reference locale package on the same data.
// The reference engine lives in the development module, so its answers arrive through testdata/locale-expected.tsv.gz.
// The differential test of dev/internal/differential regenerates that file and checks that it is current.

import (
	"bufio"
	"compress/gzip"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// LocaleCountEmbedded opens one real locale to reach the loaded section table, then counts.
func LocaleCountEmbedded(t *testing.T) int {
	l, err := OpenLocale("en", "")
	if err != nil {
		t.Fatal("en locale missing")
	}
	return LocaleCount(&l)
}

// expectedLines yields the fields of every data line of the locale answer file.
func expectedLines(t *testing.T) [][]string {
	t.Helper()
	f, err := os.Open("testdata/locale-expected.tsv.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]string
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, strings.Split(line, "\t"))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("the locale answer file is empty")
	}
	return lines
}

func parseHex(t *testing.T, field string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(field, 16, 64)
	if err != nil {
		t.Fatalf("bad hex field %q: %v", field, err)
	}
	return v
}

func parseSeq(t *testing.T, field string) []int32 {
	t.Helper()
	if field == "" {
		return []int32{}
	}
	var out []int32
	for _, part := range strings.Split(field, ",") {
		out = append(out, int32(parseHex(t, part)))
	}
	return out
}

func parseBool(t *testing.T, field string) bool {
	t.Helper()
	v, err := strconv.ParseBool(field)
	if err != nil {
		t.Fatalf("bad boolean field %q: %v", field, err)
	}
	return v
}

func parseInt(t *testing.T, field string) int {
	t.Helper()
	v, err := strconv.Atoi(field)
	if err != nil {
		t.Fatalf("bad integer field %q: %v", field, err)
	}
	return v
}

// TestLocaleAgainstReference replays the answer file: locale names, the count, class masks, case mappings, preimages and collation.
func TestLocaleAgainstReference(t *testing.T) {
	var cur Locale
	var curName string
	skipping := false
	for _, f := range expectedLines(t) {
		switch f[0] {
		case "count":
			if got := LocaleCountEmbedded(t); got != parseInt(t, f[1]) {
				t.Fatalf("locale count: reference %s, engine %d", f[1], got)
			}
		case "open":
			name := f[1]
			_, err := OpenLocale(name, "")
			if ok := err == nil; ok != parseBool(t, f[2]) {
				t.Fatalf("OpenLocale(%q): reference ok=%s, engine ok=%v", name, f[2], ok)
			}
			if (name == "cs.u-t-f-8" || name == "cs.UTF--8" || name == "cs.UTF-8-") && err == nil {
				t.Fatalf("OpenLocale(%q) accepted a malformed UTF-8 suffix", name)
			}
		case "locale":
			l, err := OpenLocale(f[1], f[2])
			if ok := err == nil; ok != parseBool(t, f[3]) {
				t.Fatalf("OpenLocale(%q,%q): reference ok=%s, engine ok=%v", f[1], f[2], f[3], ok)
			}
			cur, curName, skipping = l, f[1], err != nil
		case "r":
			if skipping {
				continue
			}
			r := int32(parseHex(t, f[1]))
			if m := localeClassMask(&cur, r); int64(m) != parseHex(t, f[2]) {
				t.Fatalf("%s: ClassMask(%#x): reference %s, engine %x", curName, r, f[2], m)
			}
			if u := localeToUpper(&cur, r); int64(u) != parseHex(t, f[3]) {
				t.Fatalf("%s: ToUpper(%#x): reference %s, engine %x", curName, r, f[3], u)
			}
			if d := localeToLower(&cur, r); int64(d) != parseHex(t, f[4]) {
				t.Fatalf("%s: ToLower(%#x): reference %s, engine %x", curName, r, f[4], d)
			}
			var buf preimageBuf
			localeCasePreimages(&cur, &buf, r)
			pre := make([]int32, 0, buf.n)
			for i := 0; i < buf.n; i++ {
				pre = append(pre, buf.r[i])
			}
			slices.Sort(pre)
			if want := parseSeq(t, f[5]); !slices.Equal(pre, want) {
				t.Fatalf("%s: preimages(%#x): reference %v, engine %v", curName, r, want, pre)
			}
		case "c":
			if skipping {
				continue
			}
			seq := parseSeq(t, f[1])
			if c := localeIsCollatingElement(&cur, seq); c != parseBool(t, f[2]) {
				t.Fatalf("%s: IsCollatingElement(%v): reference %s, engine %v", curName, seq, f[2], c)
			}
			if p := LocaleCollatingPrefix(&cur, seq); p != parseInt(t, f[3]) {
				t.Fatalf("%s: CollatingPrefix(%v): reference %s, engine %d", curName, seq, f[3], p)
			}
			if m := localeMinEquivLength(&cur, seq); m != parseInt(t, f[4]) {
				t.Fatalf("%s: MinEquivLength(%v): reference %s, engine %d", curName, seq, f[4], m)
			}
		case "e":
			if skipping {
				continue
			}
			seq, other := parseSeq(t, f[1]), parseSeq(t, f[2])
			if e := localePrimaryEqual(&cur, seq, other); e != parseBool(t, f[3]) {
				t.Fatalf("%s: PrimaryEqual(%v,%v): reference %s, engine %v", curName, seq, other, f[3], e)
			}
		default:
			t.Fatalf("unknown line kind %q", f[0])
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
			r0, size0 := stdlibDecodeRune(s[at:])
			if r0 != r1 || size0 != size1 {
				t.Fatalf("decode %q at %d: stdlib (%#x,%d), engine (%#x,%d)",
					s, at, r0, size0, r1, size1)
			}
			at += size1
		}
	}
}

// stdlibDecodeRune mirrors decodeRune with the stdlib decoder.
// An invalid sequence maps to the sentinel, and an encoded U+FFFD stays a three-byte character.
func stdlibDecodeRune(s string) (rune, int) {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size <= 1 {
		return -1, 1
	}
	return r, size
}
