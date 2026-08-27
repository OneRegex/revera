// Command probecheck checks the target-language probe binaries against the Go probe package.
// The probe covers the Vego constructs that the engine never uses, so the printers stay correct beyond the engine.
//
// Usage:
//
//	probecheck probe-binary...
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"revera1/probe"
)

func main() {
	expected := probe.ReportLines()
	failed := false
	for _, bin := range os.Args[1:] {
		var out bytes.Buffer
		cmd := exec.Command(bin)
		cmd.Stdout = &out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("%s: FAILED to run: %v\n", bin, err)
			failed = true
			continue
		}
		got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
		bad := 0
		for i := range expected {
			if i >= len(got) || got[i] != expected[i] {
				gotLine := "<missing>"
				if i < len(got) {
					gotLine = got[i]
				}
				fmt.Printf("%s: line %d\n  want: %s\n  got:  %s\n", bin, i+1, expected[i], gotLine)
				bad++
			}
		}
		if len(got) > len(expected) {
			fmt.Printf("%s: %d extra lines\n", bin, len(got)-len(expected))
			bad++
		}
		if bad > 0 {
			fmt.Printf("%s: FAIL\n", bin)
			failed = true
		} else {
			fmt.Printf("%s: OK (%d lines)\n", bin, len(expected))
		}
	}
	if failed {
		os.Exit(1)
	}
}
