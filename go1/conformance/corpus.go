// Package conformance is the backend conformance kit.
// It builds the differential corpus, runs target drivers and probes against the Go engine, writes the fuzz seed pack, and drives the whole check for a backend described by a manifest.
// cmd/crosscheck, cmd/probecheck and the conform command of cmd/revera are thin layers over it.
package conformance

import (
	"fmt"
	"math/rand"
	"strings"

	"revera1/revera"
)

// CorpusOptions selects the size of the differential corpus.
type CorpusOptions struct {
	// Quick divides every random block by ten.
	Quick bool
	// Extra adds random rounds of 500 patterns each beyond the fixed corpus.
	Extra int64
	// ExtraSeed is the seed of the first extra round.
	// The seeds of the following rounds count up from it.
	ExtraSeed int64
	// Light leaves out the executions of the heaviest fixed pattern and keeps its compile and contract commands.
	// The Lean replay makes the same cut; checked builds use it because those executions dominate their run time.
	Light bool
}

// Corpus is a list of driver protocol commands with the answers of the Go engine.
type Corpus struct {
	Commands []string
	Expected []string
}

type builder struct {
	quick bool
	light bool
	lines []string
}

func (b *builder) add(format string, args ...any) {
	b.lines = append(b.lines, fmt.Sprintf(format, args...))
}

func enc(s string) string {
	return revera.DriverEncode(s)
}

// compile emits a C command.
// Every exec-shaped command that follows applies to this pattern.
func (b *builder) compile(flags uint32, pattern string) {
	b.add("C %d %s", flags, enc(pattern))
}

func (b *builder) exec(eflags uint32, subject string) {
	b.add("X %d %s", eflags, enc(subject))
}

func (b *builder) random(seed int64, rounds int, cflags uint32, alphabet string) {
	if b.quick {
		rounds /= 10
	}
	rng := rand.New(rand.NewSource(seed))
	for range rounds {
		pattern := revera.GenPattern(rng, 3)
		b.compile(cflags, pattern)
		for range 6 {
			subject := revera.GenSubject(rng, alphabet, 7)
			eflags := uint32(rng.Intn(4))
			b.exec(eflags, subject)
		}
	}
}

func (b *builder) fixed() {
	for _, cflags := range revera.FixedFlagSets {
		for _, pattern := range revera.FixedPatterns {
			b.compile(cflags, pattern)
			if !b.light || pattern != revera.HeavyPattern {
				for _, subject := range revera.FixedSubjects {
					for eflags := uint32(0); eflags < 4; eflags++ {
						b.exec(eflags, subject)
					}
				}
			}
			b.add("T %d", 1<<12)
			b.add("T %d", 16)
		}
	}
}

func (b *builder) multiElement() {
	rng := rand.New(rand.NewSource(5))
	b.add("L %s %s", enc("cs"), enc(""))
	for _, cflags := range []uint32{0, revera.FlagICase, revera.FlagMinimal} {
		for _, pattern := range revera.MultiElementPatterns {
			b.compile(cflags, pattern)
			for range 40 {
				subject := revera.GenSubject(rng, "chxCH", 6)
				b.exec(0, subject)
			}
		}
	}
	b.add("O 0 1024")
	b.add("P")
}

func (b *builder) replace() {
	// This is the shared corpus, plus the replacement-error shapes that the Go differential cannot observe.
	// go0 has no positioned errors to compare there.
	cases := append([]revera.ReplaceCase{}, revera.ReplaceCases...)
	cases = append(cases,
		revera.ReplaceCase{Pattern: "(a+)(b+)", Subject: "aabb xab", Replacement: `\9`, Limit: -1},
		revera.ReplaceCase{Pattern: "a", Subject: "aaaa", Replacement: `x\`, Limit: -1})
	for _, cc := range cases {
		b.compile(0, cc.Pattern)
		for eflags := uint32(0); eflags < 4; eflags++ {
			b.add("R %d %d %s %s", cc.Limit, eflags, enc(cc.Replacement), enc(cc.Subject))
		}
	}
	b.compile(revera.FlagNoSub, "a")
	for _, replacement := range []string{"-", `\`, `\1`} {
		for eflags := uint32(0); eflags < 4; eflags++ {
			b.add("R -1 %d %s %s", eflags, enc(replacement), enc("a"))
		}
	}
}

func (b *builder) iter() {
	for _, pattern := range revera.IterPatterns {
		if pattern == "" {
			continue
		}
		b.compile(0, pattern)
		for _, subject := range revera.IterSubjects {
			for _, limit := range []int{-1, 0, 1, 2} {
				for eflags := uint32(0); eflags < 4; eflags++ {
					b.add("I %d %d %s", limit, eflags, enc(subject))
				}
			}
		}
	}
}

func (b *builder) locales() {
	// These are case-map sweeps across the BMP start, plus one wide sweep.
	// They cover a set of locales with distinct case behavior.
	names := []string{"tr", "az", "el", "de", "fr", "cs", "en", "nosuchlocale"}
	b.add("O 0 4096")
	// This is locale-sensitive matching.
	// The digest sweep alone could hide a divergence behind a hash collision.
	// Each locale therefore also compiles and runs case-insensitive patterns over subjects with interesting case behavior.
	for _, n := range names {
		b.add("L %s %s", enc(n), enc(""))
		b.add("O 0 2048")
		b.add("O 7680 8192")
		for _, p := range revera.LocalePatterns {
			b.compile(revera.FlagICase, p)
			for _, s := range revera.LocaleSubjects {
				b.exec(0, s)
			}
		}
	}
	b.add("P")
	b.add("O 0 65536")
	// The drivers convert the O bounds to int32 before they iterate.
	// These bounds sit outside that range on purpose.
	b.add("O -2147483649 -2147483648")
	b.add("O 2147483646 2147483650")
}

// The extra rounds walk fresh seeds over rotating flag sets and alphabets.
var (
	extraFlagSets = []uint32{0, revera.FlagICase, revera.FlagNewline, revera.FlagMinimal,
		revera.FlagICase | revera.FlagMinimal, revera.FlagNewline | revera.FlagMinimal, revera.FlagNoSub}
	extraAlphabets = []string{"abc", "ab\nc", "abcABC", "abAB"}
)

// DefaultExtraSeed is the seed of the first extra round of cmd/crosscheck.
const DefaultExtraSeed = 100

// CommandLines builds the corpus commands without running them.
func CommandLines(opts CorpusOptions) []string {
	b := &builder{quick: opts.Quick, light: opts.Light}
	b.random(1, 3000, 0, "abc")
	b.random(6, 1000, 0, "ab\nc")
	b.random(2, 1500, revera.FlagICase, "abcABC")
	b.random(3, 1500, revera.FlagNewline, "ab\nc")
	b.random(4, 1500, revera.FlagMinimal, "abc")
	b.random(7, 1000, revera.FlagICase|revera.FlagMinimal, "abAB")
	b.fixed()
	b.multiElement()
	b.replace()
	b.iter()
	b.locales()
	b.lines = append(b.lines, StressLines(opts.ExtraSeed, opts.Extra, opts.Quick)...)
	return b.lines
}

// StressLines builds rounds of random commands beyond the fixed corpus.
// Round i uses seed+i, and the seed alone selects the flag set and the alphabet.
// A run can therefore extend an earlier one by starting at the next seed, without repeating a round.
func StressLines(seed int64, rounds int64, quick bool) []string {
	b := &builder{quick: quick}
	for i := int64(0); i < rounds; i++ {
		s := seed + i
		b.random(s, 500, extraFlagSets[rotate(s, len(extraFlagSets))],
			extraAlphabets[rotate(s, len(extraAlphabets))])
	}
	return b.lines
}

// rotate maps a seed onto an index, for negative seeds too.
func rotate(seed int64, n int) int64 {
	idx := seed % int64(n)
	if idx < 0 {
		idx += int64(n)
	}
	return idx
}

// Answer runs commands through the Go engine and pairs them with their answers.
func Answer(commands []string) Corpus {
	session := revera.NewDriverSession()
	expected := make([]string, len(commands))
	for i, line := range commands {
		expected[i] = session.Eval(line)
	}
	return Corpus{Commands: commands, Expected: expected}
}

// BuildCorpus builds the corpus and answers it with the Go engine.
func BuildCorpus(opts CorpusOptions) Corpus {
	return Answer(CommandLines(opts))
}

// Input joins the commands into the text a driver reads on stdin.
func (c Corpus) Input() string {
	return strings.Join(c.Commands, "\n") + "\n"
}

// Dump formats the corpus as tab-separated command and answer pairs.
// lean/data/corpus.tsv holds this text, and the Lean theorems replay it.
func (c Corpus) Dump() string {
	var b strings.Builder
	for i := range c.Commands {
		b.WriteString(c.Commands[i])
		b.WriteByte('\t')
		b.WriteString(c.Expected[i])
		b.WriteByte('\n')
	}
	return b.String()
}
