package differential

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

var updateExpected = flag.Bool("update", false, "rewrite go/testdata/locale-expected.tsv.gz from the reference engine")

// expectedPath is the locale answer file that the white-box locale test of the engine module reads.
const expectedPath = "../../../go/testdata/locale-expected.tsv.gz"

// localeSamples are the locale selections the answer file covers.
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

// openSamples are the names whose acceptance the answer file records, including malformed ones.
var openSamples = []string{"C", "POSIX", "c.UTF-8", "en", "EN_us", "xx-not-there",
	"de@collation=phonebook", "de@bad", "cs.utf8", "cs.UTF8", "cs.u-t-f-8",
	"cs.UTF--8", "cs.UTF-8-", "cs.latin2", "",
	"tr", "sv", "de@collation=dictionary"}

// collationSamples are the sequences the collation lines cover.
var collationSamples = [][]int32{
	{'c', 'h'}, {'C', 'H'}, {'c'}, {'x', 'y'}, {'d', 'z', 's'},
	{'a', 'e'}, {0x153}, {'s', 's'}, {'l', 'l'}, {'i', 'j'},
	{'t', 'h'}, {'n', 'g'}, {'a'}, {},
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

func hexSeq(seq []int32) string {
	parts := make([]string, len(seq))
	for i, r := range seq {
		parts[i] = fmt.Sprintf("%x", r)
	}
	return strings.Join(parts, ",")
}

// generateExpected asks the reference locale package every question the engine test repeats.
// The line format is documented at the top of the file it produces.
func generateExpected() []byte {
	var b strings.Builder
	b.WriteString("# Locale answers of the reference engine, written by go test ./internal/differential -run TestLocaleExpectedData -update in dev/.\n")
	b.WriteString("# Do not edit.\n")
	b.WriteString("# count N: number of embedded locales.\n")
	b.WriteString("# open NAME OK: whether the name opens with an empty collation type.\n")
	b.WriteString("# locale NAME CTYPE: the following lines refer to this selection.\n")
	b.WriteString("# r RUNE MASK UPPER LOWER PREIMAGES: class mask, case mappings and sorted case preimages of one rune, in hex.\n")
	b.WriteString("# c SEQ COLL PREFIX MIN: collating element, collating prefix length and minimum equivalent length of one sequence.\n")
	b.WriteString("# e SEQ OTHER EQUAL: primary equality of two sequences.\n")
	fmt.Fprintf(&b, "count\t%d\n", locale.Count())
	for _, name := range openSamples {
		_, ok := locale.Open(name, "")
		fmt.Fprintf(&b, "open\t%s\t%t\n", name, ok)
	}
	runes := sampleRunes()
	for _, sel := range localeSamples {
		l, ok := locale.Open(sel.name, sel.ctype)
		fmt.Fprintf(&b, "locale\t%s\t%s\t%t\n", sel.name, sel.ctype, ok)
		if !ok {
			continue
		}
		for _, r := range runes {
			pre := l.AppendCasePreimages(nil, r)
			slices.Sort(pre)
			fmt.Fprintf(&b, "r\t%x\t%x\t%x\t%x\t%s\n", r, l.ClassMask(r), l.ToUpper(r), l.ToLower(r), hexSeq(pre))
		}
		for _, seq := range collationSamples {
			fmt.Fprintf(&b, "c\t%s\t%t\t%d\t%d\n", hexSeq(seq), l.IsCollatingElement(seq), l.CollatingPrefix(seq), l.MinEquivLength(seq))
			for _, other := range collationSamples {
				fmt.Fprintf(&b, "e\t%s\t%s\t%t\n", hexSeq(seq), hexSeq(other), l.PrimaryEqual(seq, other))
			}
		}
	}
	return []byte(b.String())
}

// TestLocaleExpectedData keeps the checked-in answer file equal to what the reference engine says today.
// With -update it rewrites the file instead.
func TestLocaleExpectedData(t *testing.T) {
	want := generateExpected()
	if *updateExpected {
		var buf bytes.Buffer
		zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := zw.Write(want); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(expectedPath, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s: %d bytes uncompressed, %d bytes compressed", expectedPath, len(want), buf.Len())
		return
	}
	f, err := os.Open(expectedPath)
	if err != nil {
		t.Fatalf("%v; run this test with -update to create the file", err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; run this test with -update", expectedPath)
	}
}
