// Command probecheck checks the target-language probe binaries against the Go probe package.
// The probe covers the Vego constructs that the engine never uses, so the printers stay correct beyond the engine.
//
// Usage:
//
//	probecheck probe-binary...
package main

import (
	"fmt"
	"os"

	"revera1/conformance"
)

func main() {
	if len(os.Args) == 1 {
		fmt.Fprintln(os.Stderr, "usage: probecheck probe-binary...")
		os.Exit(2)
	}
	failed := false
	for _, bin := range os.Args[1:] {
		r := conformance.RunProbe(bin)
		fmt.Print(r.Text)
		failed = failed || r.Failed
	}
	if failed {
		os.Exit(1)
	}
}
