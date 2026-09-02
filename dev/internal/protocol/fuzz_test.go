package protocol

import (
	"bytes"
	"testing"

	"github.com/oneregex/revera/go"
)

func TestFuzzInputRoundTrip(t *testing.T) {
	in := FuzzInput{Flags: revera.FlagICase | revera.FlagMinimal, EFlags: 2, Locale: "tr", Pattern: "(a|b)*", Replacement: `\1`, Subject: "abab"}
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
