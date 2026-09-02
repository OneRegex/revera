// Command proberef prints the probe results of the Go original.
// The Zig, C++ and Rust probe binaries print the same lines, and the probecheck harness diffs them.
package main

import (
	"fmt"

	"github.com/oneregex/revera/vego/probe"
)

func main() {
	for _, line := range probe.ReportLines() {
		fmt.Println(line)
	}
}
