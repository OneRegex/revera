// Command fuzzcase runs a fuzz seed pack through the Go engine.
// It is the reference for the fuzzcase binaries of the targets: same pack format, same procedure, same count line.
//
// Usage:
//
//	fuzzcase pack-file
package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/oneregex/revera/dev/internal/protocol"
	"github.com/oneregex/revera/go"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fuzzcase pack-file")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()
	inputs, err := protocol.ReadFuzzPack(bufio.NewReader(f))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	base, ok := revera.LocaleLoad(revera.EmbeddedLocaleData())
	if !ok {
		fmt.Fprintln(os.Stderr, "embedded locale data failed to load")
		os.Exit(1)
	}
	for _, in := range inputs {
		protocol.FuzzRun(&base, in)
	}
	fmt.Printf("fuzzcase: %d inputs\n", len(inputs))
}
