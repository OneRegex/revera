package bench

import (
	"strings"

	reference "github.com/oneregex/revera/dev/internal/reference"
	"github.com/oneregex/revera/dev/internal/reference/locale"

	"github.com/oneregex/revera/dev/internal/protocol"
)

// referenceSession runs the bench protocol with the reference engine.
// It answers the same lines as protocol.BenchSession, so the two engines share one report.
type referenceSession struct {
	cur locale.Locale
}

func newReferenceSession() *referenceSession {
	return &referenceSession{cur: locale.POSIX()}
}

func (s *referenceSession) Eval(line string) string {
	f := strings.Fields(line)
	switch f[0] {
	case "P":
		s.cur = locale.POSIX()
		return "P 1"
	case "L":
		loc, ok := locale.Open(protocol.DriverDecode(f[1]), protocol.DriverDecode(f[2]))
		if !ok {
			return "L 0"
		}
		s.cur = loc
		return "L 1"
	case "B":
		req := protocol.ParseBenchCommand(f)
		flags := reference.CompileFlags(req.Flags)
		re, err := reference.Compile(req.Pattern, s.cur, flags)
		if err != nil {
			return protocol.BenchFailure(req.Name, int32(err.(*reference.Error).Code))
		}
		var op func()
		switch req.Kind {
		case protocol.BenchCompile:
			loc := s.cur
			op = func() { _, _ = reference.Compile(req.Pattern, loc, flags) }
		case protocol.BenchMatch:
			groups := re.NumSub() + 1
			op = func() {
				pmatch := make([]reference.Match, groups)
				_, _ = re.Exec(req.Subject, pmatch, 0)
			}
		case protocol.BenchReplace:
			op = func() { _, _ = re.ReplaceAll(req.Subject, req.Replacement, -1, 0) }
		default:
			panic("unknown bench kind " + req.Kind)
		}
		return protocol.BenchAnswer(req.Name, op, req.Iters, req.Reps)
	}
	panic("unknown bench command " + f[0])
}
