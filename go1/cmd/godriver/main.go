// Command godriver runs the driver protocol with the Go engine, to help debug the cross-language drivers.
package main

import (
	"bufio"
	"fmt"
	"os"

	"revera1/revera"
)

func main() {
	s := revera.NewDriverSession()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for sc.Scan() {
		if len(sc.Text()) == 0 {
			continue
		}
		fmt.Fprintln(w, s.Eval(sc.Text()))
	}
}
