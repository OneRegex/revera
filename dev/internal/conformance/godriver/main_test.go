package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/oneregex/revera/dev/internal/protocol"
)

func TestRunAcceptsProtocolLinesAboveOneMiB(t *testing.T) {
	subject := strings.Repeat("61", 524289)
	input := "C 0 61\nX 0 " + subject + "\n"
	var out bytes.Buffer
	if err := run(protocol.NewDriverSession(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "C 0 0 0\nX 0 1 0,1\n"; got != want {
		t.Fatalf("driver output = %q, want %q", got, want)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("input failed")
}

func TestRunReportsInputErrors(t *testing.T) {
	if err := run(protocol.NewDriverSession(), failingReader{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "input failed") {
		t.Fatalf("run error = %v", err)
	}
}
