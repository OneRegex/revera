package protocol

// Host file, outside the Vego subset.
// It holds the shared benchmark cases and the reference side of the bench protocol.
// The Rust, Zig and C++ bench binaries implement the same line protocol.
// cmd/bench feeds every binary the same cases and reports the results side by side.
//
// Commands, one per line, strings hex-encoded with "-" for empty:
//
//	P                                  -> P 1
//	L <namehex> <collhex>              -> L <ok>
//	B <name> <kind> <iters> <reps> <cflags> <patternhex> <subjecthex> <replhex>
//	                                   -> B <name> <code> <bytes> <allocs> <ns>...
//
// A B command times <iters> operations of <kind> on the current locale, <reps> times.
// The kinds are compile, match and replace.
// Match and replace compile the pattern once, outside the timed region.
// A match operation allocates its match buffer, as the public APIs do.
// The answer gives the compile code, the bytes and allocations of one operation, and the total nanoseconds of each repetition.
// A failed compile answers the code with zero figures and no timings.

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/oneregex/revera/go"
)

const (
	BenchCompile = "compile"
	BenchMatch   = "match"
	BenchReplace = "replace"
)

// BenchGroup is one section of the report, in report order.
type BenchGroup struct {
	Name  string
	Title string
}

var BenchGroups = []BenchGroup{
	{"compile", "compile time, ns per Compile"},
	{"match", "match time, ns per Exec with captures"},
	{"hard", "difficult patterns, ns per Exec with captures"},
	{"replace", "replacement, ns per ReplaceAll"},
}

// BenchCase is one shared benchmark.
// Group names the report section, Kind names the timed operation.
type BenchCase struct {
	Name        string
	Group       string
	Kind        string
	Locale      string
	Flags       uint32
	Pattern     string
	Subject     string
	Replacement string
	Iters       int
}

// Key is the name of the case on the wire and in reports; names repeat across groups.
func (c BenchCase) Key() string {
	return c.Group + "/" + c.Name
}

var benchWords = []string{
	"the", "quick", "brown", "fox", "jumps", "over", "lazy", "dog",
	"while", "seven", "judges", "examine", "vexed", "quiz", "boxes",
	"and", "one", "hundred", "twenty", "three", "engines", "agree",
}

// benchText builds a deterministic pseudo-English text of about n bytes.
// It uses its own generator so the text stays the same across Go releases.
func benchText(seed uint32, n int) string {
	var b strings.Builder
	x := seed
	for b.Len() < n {
		x = x*1664525 + 1013904223
		word := benchWords[(x>>16)%uint32(len(benchWords))]
		if b.Len() > 0 {
			switch (x >> 8) % 16 {
			case 0:
				b.WriteString(". ")
			case 1:
				b.WriteString(", ")
			case 2:
				b.WriteString(" ")
				b.WriteString(strconv.Itoa(int((x >> 24) % 1000)))
				b.WriteString(" ")
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteString(word)
	}
	return b.String()
}

var benchWordAlternation = "(" + strings.Join([]string{
	"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
	"india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa",
	"quebec", "romeo", "sierra", "tango",
}, "|") + ")"

// BenchCases returns the shared benchmark list.
// The iteration counts give each repetition a few milliseconds on a 2025 laptop.
func BenchCases() []BenchCase {
	text := benchText(1, 1000)
	hit := text + " needle"
	emails := strings.Repeat("contact alice@example.org or bob@example.com now. ", 20)
	aaa := func(n int) string { return strings.Repeat("a", n) }
	cases := []BenchCase{
		{Name: "literal", Group: "compile", Kind: BenchCompile, Pattern: "needle", Iters: 20000},
		{Name: "groups", Group: "compile", Kind: BenchCompile, Pattern: "([a-z]+)([0-9]*)", Iters: 10000},
		{Name: "alternation", Group: "compile", Kind: BenchCompile, Pattern: "(wee|week)(knights|night)", Iters: 5000},
		{Name: "classes", Group: "compile", Kind: BenchCompile, Pattern: "[[:alpha:]][[:alnum:]_]*[[:space:]]*", Iters: 10000},
		{Name: "words", Group: "compile", Kind: BenchCompile, Pattern: benchWordAlternation, Iters: 2000},
		{Name: "counted", Group: "compile", Kind: BenchCompile, Pattern: "(a{0,2}){2,3}", Iters: 10000},
		{Name: "counted-255", Group: "compile", Kind: BenchCompile, Pattern: "a{255}", Iters: 2000},
		{Name: "nested-counted", Group: "compile", Kind: BenchCompile, Pattern: "((a*){4}){4}", Iters: 5000},
		{Name: "icase-utf8", Group: "compile", Kind: BenchCompile, Flags: revera.FlagICase, Pattern: "é(è|e)*", Iters: 10000},
		{Name: "cs-collating", Group: "compile", Kind: BenchCompile, Locale: "cs", Pattern: "([[.ch.]]|c)h?", Iters: 5000},

		{Name: "literal-hit", Group: "match", Kind: BenchMatch, Pattern: "needle", Subject: hit, Iters: 2000},
		{Name: "literal-miss", Group: "match", Kind: BenchMatch, Pattern: "needle", Subject: text, Iters: 2000},
		{Name: "groups-short", Group: "match", Kind: BenchMatch, Pattern: "([a-z]+)([0-9]*)", Subject: "__abc12__", Iters: 10000},
		{Name: "groups-long", Group: "match", Kind: BenchMatch, Pattern: "([a-z]+)-([a-z]+)-([0-9]+)", Subject: text + " alpha-beta-42", Iters: 500},
		{Name: "nosub-long", Group: "match", Kind: BenchMatch, Flags: revera.FlagNoSub, Pattern: "([a-z]+)-([a-z]+)-([0-9]+)", Subject: text + " alpha-beta-42", Iters: 500},
		{Name: "words", Group: "match", Kind: BenchMatch, Pattern: benchWordAlternation, Subject: text + " tango", Iters: 100},
		{Name: "classes", Group: "match", Kind: BenchMatch, Pattern: "[[:alpha:]]+ [[:digit:]]+ [[:alpha:]]+", Subject: text, Iters: 2000},
		{Name: "anchored", Group: "match", Kind: BenchMatch, Pattern: "^[^z]*$", Subject: text, Iters: 2000},
		{Name: "dot-star", Group: "match", Kind: BenchMatch, Pattern: ".*", Subject: text, Iters: 1000},
		{Name: "icase", Group: "match", Kind: BenchMatch, Flags: revera.FlagICase, Pattern: "NEEDLE", Subject: hit, Iters: 1000},
		{Name: "newline", Group: "match", Kind: BenchMatch, Flags: revera.FlagNewline, Pattern: "^[a-z]+$", Subject: strings.ReplaceAll(text, ". ", "\n") + "\nneedle", Iters: 1000},
		{Name: "minimal", Group: "match", Kind: BenchMatch, Flags: revera.FlagMinimal, Pattern: "(a*?)c", Subject: aaa(100) + "c", Iters: 5000},
		{Name: "utf8", Group: "match", Kind: BenchMatch, Pattern: ".€.", Subject: "prix: 12€50, taxe: 2€30", Iters: 20000},
		{Name: "cs-collating", Group: "match", Kind: BenchMatch, Locale: "cs", Pattern: "([[.ch.]]|[ch])*", Subject: "chchhcchxchhc", Iters: 5000},
		{Name: "tr-icase", Group: "match", Kind: BenchMatch, Locale: "tr", Flags: revera.FlagICase, Pattern: "[[:alpha:]]+", Subject: "IİıiIi kelime", Iters: 5000},

		{Name: "nested-star", Group: "hard", Kind: BenchMatch, Pattern: "(a*)*b", Subject: aaa(30), Iters: 2000},
		{Name: "double-plus", Group: "hard", Kind: BenchMatch, Pattern: "(x+x+)+y", Subject: strings.Repeat("x", 30), Iters: 500},
		{Name: "nested-counted", Group: "hard", Kind: BenchMatch, Pattern: "((a*){4}){4}", Subject: aaa(16), Iters: 100},
		{Name: "five-dot-stars", Group: "hard", Kind: BenchMatch, Pattern: "(.*)(.*)(.*)(.*)(.*)", Subject: strings.Repeat("ab", 30), Iters: 20},
		{Name: "counted-255", Group: "hard", Kind: BenchMatch, Pattern: "a{255}", Subject: aaa(255), Iters: 50},
		{Name: "re2-classic", Group: "hard", Kind: BenchMatch, Pattern: "[a-q][^u-z]{13}x", Subject: text, Iters: 500},
		{Name: "ambiguous-groups", Group: "hard", Kind: BenchMatch, Pattern: "((a|ab)(c|bcd)(d*))+", Subject: strings.Repeat("abcd", 20), Iters: 50},
		{Name: "many-groups", Group: "hard", Kind: BenchMatch, Pattern: "((a)(b)(c)(d)(e)(f)(g)(h)(i)(j))+", Subject: strings.Repeat("abcdefghij", 30), Iters: 200},
		{Name: "empty-loops", Group: "hard", Kind: BenchMatch, Pattern: "(a?)*b", Subject: aaa(30), Iters: 2000},
		{Name: "capacity-fallback", Group: "hard", Kind: BenchMatch, Pattern: HeavyPattern, Subject: aaa(30), Iters: 2},

		{Name: "literal", Group: "replace", Kind: BenchReplace, Pattern: "the", Subject: text, Replacement: "THE", Iters: 500},
		{Name: "groups", Group: "replace", Kind: BenchReplace, Pattern: "([a-z]+)@([a-z]+)", Subject: emails, Replacement: `\2 at \1`, Iters: 200},
		{Name: "empty-matches", Group: "replace", Kind: BenchReplace, Pattern: "a*", Subject: text, Replacement: "-", Iters: 200},
		{Name: "no-match", Group: "replace", Kind: BenchReplace, Pattern: "zzz", Subject: text, Replacement: "y", Iters: 2000},
	}
	return cases
}

// BenchCommand encodes a case as a B command line, named by its key.
func BenchCommand(c BenchCase, reps int) string {
	return fmt.Sprintf("B %s %s %d %d %d %s %s %s", c.Key(), c.Kind, c.Iters, reps, c.Flags,
		DriverEncode(c.Pattern), DriverEncode(c.Subject), DriverEncode(c.Replacement))
}

// BenchRequest is one decoded B command.
type BenchRequest struct {
	Name        string
	Kind        string
	Iters       int
	Reps        int
	Flags       uint32
	Pattern     string
	Subject     string
	Replacement string
}

// ParseBenchCommand decodes the fields of a B command.
// A malformed line is a protocol violation and panics, like the driver session.
func ParseBenchCommand(f []string) BenchRequest {
	if len(f) != 9 {
		panic("malformed bench command")
	}
	return BenchRequest{
		Name:        f[1],
		Kind:        f[2],
		Iters:       driverInt(f[3]),
		Reps:        driverInt(f[4]),
		Flags:       uint32(driverInt(f[5])),
		Pattern:     DriverDecode(f[6]),
		Subject:     DriverDecode(f[7]),
		Replacement: DriverDecode(f[8]),
	}
}

// BenchAnswer measures op and formats the B answer.
// One untimed pass of iters operations counts the heap bytes and allocations.
// Each timed repetition starts after a collection, so garbage of the earlier passes does not land inside it.
func BenchAnswer(name string, op func(), iters int, reps int) string {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range iters {
		op()
	}
	runtime.ReadMemStats(&after)
	var b strings.Builder
	fmt.Fprintf(&b, "B %s 0 %d %d", name,
		int64(after.TotalAlloc-before.TotalAlloc)/int64(iters),
		int64(after.Mallocs-before.Mallocs)/int64(iters))
	for range reps {
		runtime.GC()
		start := time.Now()
		for range iters {
			op()
		}
		fmt.Fprintf(&b, " %d", time.Since(start).Nanoseconds())
	}
	return b.String()
}

// BenchFailure formats the answer to a B command whose pattern did not compile.
func BenchFailure(name string, code int32) string {
	return fmt.Sprintf("B %s %d 0 0", name, code)
}

// BenchResult is one parsed answer to a B command.
type BenchResult struct {
	Name   string
	Code   int32
	Bytes  int64
	Allocs int64
	Nanos  []int64
}

// ParseBenchResult decodes a B answer line.
func ParseBenchResult(line string) (BenchResult, error) {
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "B" {
		return BenchResult{}, fmt.Errorf("malformed bench answer %q", line)
	}
	r := BenchResult{Name: f[1]}
	code, err := strconv.ParseInt(f[2], 10, 32)
	if err != nil {
		return BenchResult{}, fmt.Errorf("malformed bench answer %q: %w", line, err)
	}
	r.Code = int32(code)
	if r.Bytes, err = strconv.ParseInt(f[3], 10, 64); err != nil {
		return BenchResult{}, fmt.Errorf("malformed bench answer %q: %w", line, err)
	}
	if r.Allocs, err = strconv.ParseInt(f[4], 10, 64); err != nil {
		return BenchResult{}, fmt.Errorf("malformed bench answer %q: %w", line, err)
	}
	for _, tok := range f[5:] {
		ns, err := strconv.ParseInt(tok, 10, 64)
		if err != nil {
			return BenchResult{}, fmt.Errorf("malformed bench answer %q: %w", line, err)
		}
		r.Nanos = append(r.Nanos, ns)
	}
	return r, nil
}

// MustEmbeddedLocale loads the locale data compiled into the binary.
// The blob is a build input, so a failure to load it is a build defect and panics.
func MustEmbeddedLocale() revera.Locale {
	base, ok := revera.LocaleLoad(revera.EmbeddedLocaleData())
	if !ok {
		panic("embedded locale data failed to load")
	}
	return base
}

// LocaleByName resolves the locale of a case or a fuzz input.
// The empty name is POSIX; any other name is selected from the loaded data.
func LocaleByName(base *revera.Locale, name string) (revera.Locale, bool) {
	if name == "" {
		return revera.LocalePOSIX(), true
	}
	return revera.LocaleSelect(base, name, "")
}

// BenchSession runs the bench protocol with the Go engine.
type BenchSession struct {
	base revera.Locale
	cur  revera.Locale
}

func NewBenchSession() *BenchSession {
	return &BenchSession{base: MustEmbeddedLocale(), cur: revera.LocalePOSIX()}
}

// Eval runs one protocol command and returns its output line.
func (s *BenchSession) Eval(line string) string {
	f := strings.Fields(line)
	switch f[0] {
	case "P":
		s.cur = revera.LocalePOSIX()
		return "P 1"
	case "L":
		loc, ok := revera.LocaleSelect(&s.base, DriverDecode(f[1]), DriverDecode(f[2]))
		if ok {
			s.cur = loc
		}
		return fmt.Sprintf("L %d", boolInt(ok))
	case "B":
		req := ParseBenchCommand(f)
		re, err := revera.Compile(req.Pattern, s.cur, req.Flags)
		if err.Code != revera.ErrNone {
			return BenchFailure(req.Name, err.Code)
		}
		var op func()
		switch req.Kind {
		case BenchCompile:
			loc := s.cur
			op = func() { _, _ = revera.Compile(req.Pattern, loc, req.Flags) }
		case BenchMatch:
			groups := revera.NumSub(&re) + 1
			op = func() {
				pmatch := make([]revera.Match, groups)
				_, _ = revera.Exec(&re, req.Subject, pmatch, 0)
			}
		case BenchReplace:
			op = func() { _, _ = revera.ReplaceAll(&re, req.Subject, req.Replacement, -1, 0) }
		default:
			panic("unknown bench kind " + req.Kind)
		}
		return BenchAnswer(req.Name, op, req.Iters, req.Reps)
	}
	panic("unknown bench command " + f[0])
}
