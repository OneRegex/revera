package conformance

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/oneregex/revera/dev/internal/protocol"
)

func TestRunProtocolSplitsLines(t *testing.T) {
	got, err := RunProtocol(fakeDriver(t, "one", "two\r", ""), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"one", "two", ""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RunProtocol = %#v, want %#v", got, want)
	}
}

// fakeDriver writes a shell script that prints fixed lines, whatever its input.
func fakeDriver(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "driver.sh")
	script := "#!/bin/sh\ncat >/dev/null\n"
	for _, line := range lines {
		script += "echo '" + line + "'\n"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunDriverReports(t *testing.T) {
	c := Corpus{Commands: []string{"P", "P"}, Expected: []string{"P 1", "P 1"}}
	ok := RunDriver(fakeDriver(t, "P 1", "P 1"), c)
	if ok.Failed || !strings.HasSuffix(ok.Text, "OK (2 lines)\n") {
		t.Fatalf("matching driver reported %+v", ok)
	}
	wrong := RunDriver(fakeDriver(t, "P 1", "P 0"), c)
	if !wrong.Failed || !strings.Contains(wrong.Text, "cmd:  P") || !strings.Contains(wrong.Text, "FAIL (1 mismatched lines)") {
		t.Fatalf("wrong driver reported %+v", wrong)
	}
	short := RunDriver(fakeDriver(t, "P 1"), c)
	if !short.Failed || !strings.Contains(short.Text, "truncated at line 2") {
		t.Fatalf("short driver reported %+v", short)
	}
	long := RunDriver(fakeDriver(t, "P 1", "P 1", "P 1"), c)
	if !long.Failed || !strings.Contains(long.Text, "1 extra output lines") {
		t.Fatalf("long driver reported %+v", long)
	}
	missing := RunDriver(filepath.Join(t.TempDir(), "nope"), c)
	if !missing.Failed || !strings.Contains(missing.Text, "FAILED to run") {
		t.Fatalf("missing driver reported %+v", missing)
	}
}

func TestCorpusMatchesLeanDump(t *testing.T) {
	if problems := LeanDataProblems(filepath.Join("..", "..", ".."), nil); len(problems) > 0 {
		t.Fatalf("the Lean data is stale: %s", strings.Join(problems, "; "))
	}
}

func TestLightCorpusKeepsCompilesAndContracts(t *testing.T) {
	full := CommandLines(CorpusOptions{Quick: true})
	light := CommandLines(CorpusOptions{Quick: true, Light: true})
	if len(light) >= len(full) {
		t.Fatal("the light corpus must drop commands")
	}
	count := func(lines []string, prefix string) int {
		n := 0
		for _, line := range lines {
			if strings.HasPrefix(line, prefix) {
				n++
			}
		}
		return n
	}
	if count(light, "C ") != count(full, "C ") || count(light, "T ") != count(full, "T ") {
		t.Fatal("the light corpus must keep every compile and contract command")
	}
	if len(full)-len(light) != len(protocol.FixedFlagSets)*len(protocol.FixedSubjects)*4 {
		t.Fatalf("the light corpus dropped %d commands", len(full)-len(light))
	}
}

func TestStressLinesExtendWithoutRepeating(t *testing.T) {
	a := StressLines(100, 2, true)
	b := StressLines(101, 1, true)
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("stress rounds produced no commands")
	}
	if !reflect.DeepEqual(a[len(a)-len(b):], b) {
		t.Fatal("round seed+1 must reproduce the second round of a two-round run")
	}
}
