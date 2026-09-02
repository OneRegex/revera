package protocol

import (
	"testing"

	"github.com/oneregex/revera/go"
)

// BenchmarkEngine runs the shared cases of cmd/bench under the testing harness.
// It exists for pprof: go test -run '^$' -bench BenchmarkEngine -benchmem -cpuprofile cpu.pprof
func BenchmarkEngine(b *testing.B) {
	base := MustEmbeddedLocale()
	for _, c := range BenchCases() {
		b.Run(c.Key(), func(b *testing.B) {
			loc, ok := LocaleByName(&base, c.Locale)
			if !ok {
				b.Fatalf("locale %q missing", c.Locale)
			}
			re, err := revera.Compile(c.Pattern, loc, c.Flags)
			if err.Code != revera.ErrNone {
				b.Fatalf("Compile(%q): code %d", c.Pattern, err.Code)
			}
			b.ReportAllocs()
			switch c.Kind {
			case BenchCompile:
				for b.Loop() {
					_, _ = revera.Compile(c.Pattern, loc, c.Flags)
				}
			case BenchMatch:
				groups := revera.NumSub(&re) + 1
				b.SetBytes(int64(len(c.Subject)))
				for b.Loop() {
					pmatch := make([]revera.Match, groups)
					_, _ = revera.Exec(&re, c.Subject, pmatch, 0)
				}
			case BenchReplace:
				b.SetBytes(int64(len(c.Subject)))
				for b.Loop() {
					_, _ = revera.ReplaceAll(&re, c.Subject, c.Replacement, -1, 0)
				}
			default:
				b.Fatalf("unknown kind %q", c.Kind)
			}
		})
	}
}
