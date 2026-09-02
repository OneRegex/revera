package reference

import (
	"strings"
	"testing"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

func benchExec(b *testing.B, pattern, subject string, nmatch int) {
	b.Helper()
	re, err := Compile(pattern, locale.POSIX(), 0)
	if err != nil {
		b.Fatal(err)
	}
	pmatch := make([]Match, nmatch)
	b.ReportAllocs()
	b.SetBytes(int64(len(subject)))
	for b.Loop() {
		if _, err := re.Exec(subject, pmatch, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLiteralTail(b *testing.B) {
	subject := strings.Repeat("abcdefgh", 512) + "needle"
	benchExec(b, "needle", subject, 1)
}

func BenchmarkAmbiguousStar(b *testing.B) {
	subject := strings.Repeat("ab", 2048) + "abb"
	benchExec(b, "(a|b)*abb", subject, 1)
}

func BenchmarkClasses(b *testing.B) {
	subject := strings.Repeat("agent 007 ", 400) + "x42y"
	benchExec(b, "[[:alpha:]][[:digit:]]+[[:alpha:]]", subject, 1)
}

func BenchmarkNoMatch(b *testing.B) {
	subject := strings.Repeat("abcdefgh", 1024)
	benchExec(b, "zz+", subject, 1)
}

func BenchmarkCaptureSmallSpan(b *testing.B) {
	subject := strings.Repeat("x", 4000) + "abcd"
	benchExec(b, "(a|ab)(c|bcd)(d*)", subject, 4)
}
