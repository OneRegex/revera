package main

import (
	"reflect"
	"testing"
)

func TestProtocolLinesKeepsExtraBlankLine(t *testing.T) {
	if got, want := protocolLines("one\ntwo\n\n"), []string{"one", "two", ""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("protocolLines = %#v, want %#v", got, want)
	}
}
