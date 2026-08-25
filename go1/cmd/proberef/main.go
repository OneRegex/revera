// Command proberef prints the probe results of the Go original.
// The Zig, C++ and Rust probe binaries print the same lines; the
// probecheck harness diffs them.
package main

import (
	"fmt"

	"revera1/probe"
)

func main() {
	for _, line := range probe.ReportLines() {
		fmt.Println(line)
	}
}
