// Command bench runs the shared benchmark cases against every engine and prints the results side by side.
// The bench package holds the logic; this file is the entry point.
//
// Usage:
//
//	bench [-repo path] [-backend dir]... [-reps n] [-scale f] [-reference] [-build] [-only prefix] [-tsv file]
//	bench size [-repo path] [-backend dir]...
package main

import (
	"os"

	"github.com/oneregex/revera/dev/internal/conformance/bench"
)

func main() {
	os.Exit(bench.Main(os.Args[1:], os.Stdout, os.Stderr))
}
