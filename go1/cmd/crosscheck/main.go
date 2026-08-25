// Command crosscheck verifies the target-language instantiations of
// the revera engine against the Go engine. It generates the same
// corpora as the go1 differential tests, encodes them as driver
// protocol commands, computes the expected output with the Go
// engine in-process, and diffs each driver's output line by line.
//
// Usage:
//
//	crosscheck [-quick] [-dump corpus.txt] driver-binary...
package main

import (
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"sync"

	"revera1/revera"
)

var quick = flag.Bool("quick", false, "run a reduced corpus")

type corpus struct {
	lines []string
}

func (c *corpus) add(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func enc(s string) string {
	return revera.DriverEncode(s)
}

// compile emits a C command. Every exec-shaped command that follows
// applies to this pattern.
func (c *corpus) compile(flags uint32, pattern string) {
	c.add("C %d %s", flags, enc(pattern))
}

func (c *corpus) exec(eflags uint32, subject string) {
	c.add("X %d %s", eflags, enc(subject))
}

func (c *corpus) random(seed int64, rounds int, cflags uint32, alphabet string) {
	if *quick {
		rounds /= 10
	}
	rng := rand.New(rand.NewSource(seed))
	for range rounds {
		pattern := revera.GenPattern(rng, 3)
		c.compile(cflags, pattern)
		for range 6 {
			subject := revera.GenSubject(rng, alphabet, 7)
			eflags := uint32(rng.Intn(4))
			c.exec(eflags, subject)
		}
	}
}

func (c *corpus) fixed() {
	for _, cflags := range revera.FixedFlagSets {
		for _, pattern := range revera.FixedPatterns {
			c.compile(cflags, pattern)
			for _, subject := range revera.FixedSubjects {
				for eflags := uint32(0); eflags < 4; eflags++ {
					c.exec(eflags, subject)
				}
			}
			c.add("T %d", 1<<12)
			c.add("T %d", 16)
		}
	}
}

func (c *corpus) multiElement() {
	rng := rand.New(rand.NewSource(5))
	c.add("L %s %s", enc("cs"), enc(""))
	for _, cflags := range []uint32{0, revera.FlagICase, revera.FlagMinimal} {
		for _, pattern := range revera.MultiElementPatterns {
			c.compile(cflags, pattern)
			for range 40 {
				subject := revera.GenSubject(rng, "chxCH", 6)
				c.exec(0, subject)
			}
		}
	}
	c.add("O 0 1024")
	c.add("P")
}

func (c *corpus) replace() {
	// The shared corpus plus replacement-error shapes the Go
	// differential cannot observe (go0 has no positioned errors to
	// compare there).
	cases := append([]revera.ReplaceCase{}, revera.ReplaceCases...)
	cases = append(cases,
		revera.ReplaceCase{Pattern: "(a+)(b+)", Subject: "aabb xab", Replacement: `\9`, Limit: -1},
		revera.ReplaceCase{Pattern: "a", Subject: "aaaa", Replacement: `x\`, Limit: -1})
	for _, cc := range cases {
		c.compile(0, cc.Pattern)
		for eflags := uint32(0); eflags < 4; eflags++ {
			c.add("R %d %d %s %s", cc.Limit, eflags, enc(cc.Replacement), enc(cc.Subject))
		}
	}
}

func (c *corpus) iter() {
	for _, pattern := range revera.IterPatterns {
		if pattern == "" {
			continue
		}
		c.compile(0, pattern)
		for _, subject := range revera.IterSubjects {
			for _, limit := range []int{-1, 0, 1, 2} {
				for eflags := uint32(0); eflags < 4; eflags++ {
					c.add("I %d %d %s", limit, eflags, enc(subject))
				}
			}
		}
	}
}

func (c *corpus) locales() {
	// Case-map sweeps across the BMP start, plus a wide sweep, for
	// a set of locales with distinct case behavior.
	names := []string{"tr", "az", "el", "de", "fr", "cs", "en", "nosuchlocale"}
	c.add("O 0 4096")
	// Locale-sensitive matching: the digest sweep alone could hide
	// a divergence behind a hash collision, so each locale also
	// compiles and executes case-insensitive patterns over
	// subjects with interesting case behavior (dotted and dotless
	// i, sharp s, final sigma).
	patterns := []string{"i+", "k", "[[:alpha:]]+", "s(a|s)*", "(σ|i)+"}
	subjects := []string{
		"IİıiIi", "KkK", "straße", "SS ss ſ", "ΣΟΦΟΣ σοφος τέλος ς",
		"Iİıi", "ABC abc", "",
	}
	for _, n := range names {
		c.add("L %s %s", enc(n), enc(""))
		c.add("O 0 2048")
		c.add("O 7680 8192")
		for _, p := range patterns {
			c.compile(revera.FlagICase, p)
			for _, s := range subjects {
				c.exec(0, s)
			}
		}
	}
	c.add("P")
	c.add("O 0 65536")
}

func build(extra int64) *corpus {
	c := &corpus{}
	c.random(1, 3000, 0, "abc")
	c.random(6, 1000, 0, "ab\nc")
	c.random(2, 1500, revera.FlagICase, "abcABC")
	c.random(3, 1500, revera.FlagNewline, "ab\nc")
	c.random(4, 1500, revera.FlagMinimal, "abc")
	c.random(7, 1000, revera.FlagICase|revera.FlagMinimal, "abAB")
	c.fixed()
	c.multiElement()
	c.replace()
	c.iter()
	c.locales()
	// Extra rounds walk fresh seeds over rotating flag sets, so
	// repeated runs can widen the random coverage without bound.
	flagSets := []uint32{0, revera.FlagICase, revera.FlagNewline, revera.FlagMinimal,
		revera.FlagICase | revera.FlagMinimal, revera.FlagNewline | revera.FlagMinimal, revera.FlagNoSub}
	alphabets := []string{"abc", "ab\nc", "abcABC", "abAB"}
	for i := int64(0); i < extra; i++ {
		seed := 100 + i
		c.random(seed, 500, flagSets[i%int64(len(flagSets))],
			alphabets[i%int64(len(alphabets))])
	}
	return c
}

func main() {
	dump := flag.String("dump", "", "write the corpus commands to a file")
	extra := flag.Int64("extra", 0, "additional random rounds, 500 patterns each")
	flag.Parse()
	c := build(*extra)
	input := strings.Join(c.lines, "\n") + "\n"

	if *dump != "" {
		if err := os.WriteFile(*dump, []byte(input), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	session := revera.NewDriverSession()
	expected := make([]string, len(c.lines))
	for i, line := range c.lines {
		expected[i] = session.Eval(line)
	}
	fmt.Printf("corpus: %d commands\n", len(c.lines))

	// The drivers are independent processes; run them together and
	// report in argument order once all finish.
	texts := make([]string, flag.NArg())
	fails := make([]bool, flag.NArg())
	var wg sync.WaitGroup
	for di, driver := range flag.Args() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			texts[di], fails[di] = runDriver(driver, input, c.lines, expected)
		}()
	}
	wg.Wait()
	failed := false
	for i, text := range texts {
		fmt.Print(text)
		failed = failed || fails[i]
	}
	if failed {
		os.Exit(1)
	}
}

// runDriver feeds one driver the corpus and diffs its output,
// returning the report text and whether it failed.
func runDriver(driver, input string, cmds, expected []string) (string, bool) {
	var b strings.Builder
	cmd := exec.Command(driver)
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(&b, "%s: FAILED to run: %v\n", driver, err)
		return b.String(), true
	}
	got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	bad := 0
	for i := range expected {
		if i >= len(got) {
			fmt.Fprintf(&b, "%s: output truncated at line %d\n", driver, i+1)
			bad++
			break
		}
		if got[i] != expected[i] {
			if bad < 10 {
				fmt.Fprintf(&b, "%s: line %d\n  cmd:  %s\n  want: %s\n  got:  %s\n",
					driver, i+1, cmds[i], expected[i], got[i])
			}
			bad++
		}
	}
	if len(got) > len(expected) {
		fmt.Fprintf(&b, "%s: %d extra output lines\n", driver, len(got)-len(expected))
		bad++
	}
	if bad > 0 {
		fmt.Fprintf(&b, "%s: FAIL (%d mismatched lines)\n", driver, bad)
		return b.String(), true
	}
	fmt.Fprintf(&b, "%s: OK (%d lines)\n", driver, len(expected))
	return b.String(), false
}
