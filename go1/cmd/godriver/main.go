// Command godriver runs the driver protocol with the Go engine, to help debug the cross-language drivers.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"revera1/revera"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	s := revera.NewDriverSession()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), maxProtocolLine())
	w := bufio.NewWriter(out)
	for sc.Scan() {
		if len(sc.Text()) == 0 {
			continue
		}
		fmt.Fprintln(w, s.Eval(sc.Text()))
	}
	if err := sc.Err(); err != nil {
		_ = w.Flush()
		return fmt.Errorf("read driver protocol: %w", err)
	}
	return w.Flush()
}

func maxProtocolLine() int {
	const needed = uint64(2)*(1<<31-1) + 256
	intMax := uint64(^uint(0) >> 1)
	if needed > intMax {
		return int(intMax)
	}
	return int(needed)
}
