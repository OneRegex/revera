package main

import (
	"reflect"
	"testing"
)

func TestHasVerificationTarget(t *testing.T) {
	if hasVerificationTarget(0, "", "") {
		t.Fatal("empty invocation has a verification target")
	}
	for _, ok := range []bool{
		hasVerificationTarget(1, "", ""),
		hasVerificationTarget(0, "corpus.txt", ""),
		hasVerificationTarget(0, "", "corpus.tsv"),
	} {
		if !ok {
			t.Fatal("driver and dump invocations must be accepted")
		}
	}
}

func TestProtocolLinesKeepsExtraBlankLine(t *testing.T) {
	if got, want := protocolLines("one\ntwo\n\n"), []string{"one", "two", ""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("protocolLines = %#v, want %#v", got, want)
	}
}
