package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"revera"
	"revera/locale"
)

func main() {
	cases, _ := os.Open("/Users/j/src/re-vera2/tmp/cases.txt")
	libc, _ := os.Open("/Users/j/src/re-vera2/tmp/libc_out.txt")
	cs := bufio.NewScanner(cases)
	ls := bufio.NewScanner(libc)
	total, agree, diff := 0, 0, 0
	for cs.Scan() && ls.Scan() {
		parts := strings.SplitN(cs.Text(), "\t", 2)
		pattern, subject := parts[0], ""
		if len(parts) > 1 {
			subject = parts[1]
		}
		expect := ls.Text()
		re, err := revera.Compile(pattern, locale.POSIX(), 0)
		if err != nil {
			if expect != "CERR" {
				fmt.Printf("COMPILE-DIFF %q: we %v, libc %s\n", pattern, err, expect)
				diff++
			}
			total++
			continue
		}
		pmatch := make([]revera.Match, re.NumSub()+1)
		ok, execErr := re.Exec(subject, pmatch, 0)
		var got string
		if execErr != nil {
			got = "XERR"
		} else if !ok {
			got = "NOMATCH"
		} else {
			var b strings.Builder
			for i := 0; i < len(pmatch) && i < 10; i++ {
				fmt.Fprintf(&b, "(%d,%d)", pmatch[i].So, pmatch[i].Eo)
			}
			got = b.String()
		}
		total++
		if got == expect {
			agree++
		} else {
			diff++
			if diff <= 15 {
				fmt.Printf("DIFF %q on %q: we %s, libc %s\n", pattern, subject, got, expect)
			}
		}
	}
	fmt.Printf("total %d agree %d diff %d\n", total, agree, diff)
}
