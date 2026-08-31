package revera

import "testing"

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
			re, err := Compile(c.Pattern, loc, c.Flags)
			if err.Code != ErrNone {
				b.Fatalf("Compile(%q): code %d", c.Pattern, err.Code)
			}
			b.ReportAllocs()
			switch c.Kind {
			case BenchCompile:
				for b.Loop() {
					_, _ = Compile(c.Pattern, loc, c.Flags)
				}
			case BenchMatch:
				groups := NumSub(&re) + 1
				b.SetBytes(int64(len(c.Subject)))
				for b.Loop() {
					pmatch := make([]Match, groups)
					_, _ = Exec(&re, c.Subject, pmatch, 0)
				}
			case BenchReplace:
				b.SetBytes(int64(len(c.Subject)))
				for b.Loop() {
					_, _ = ReplaceAll(&re, c.Subject, c.Replacement, -1, 0)
				}
			default:
				b.Fatalf("unknown kind %q", c.Kind)
			}
		})
	}
}
