package main

import (
	"strings"

	g0 "revera"
	g0loc "revera/locale"

	"revera1/revera"
)

// go0Session runs the bench protocol with the go0 reference engine.
// It answers the same lines as revera.BenchSession, so the two engines share one report.
type go0Session struct {
	cur g0loc.Locale
}

func newGo0Session() *go0Session {
	return &go0Session{cur: g0loc.POSIX()}
}

func (s *go0Session) Eval(line string) string {
	f := strings.Fields(line)
	switch f[0] {
	case "P":
		s.cur = g0loc.POSIX()
		return "P 1"
	case "L":
		loc, ok := g0loc.Open(revera.DriverDecode(f[1]), revera.DriverDecode(f[2]))
		if !ok {
			return "L 0"
		}
		s.cur = loc
		return "L 1"
	case "B":
		req := revera.ParseBenchCommand(f)
		flags := g0.CompileFlags(req.Flags)
		re, err := g0.Compile(req.Pattern, s.cur, flags)
		if err != nil {
			return revera.BenchFailure(req.Name, int32(err.(*g0.Error).Code))
		}
		var op func()
		switch req.Kind {
		case revera.BenchCompile:
			loc := s.cur
			op = func() { _, _ = g0.Compile(req.Pattern, loc, flags) }
		case revera.BenchMatch:
			groups := re.NumSub() + 1
			op = func() {
				pmatch := make([]g0.Match, groups)
				_, _ = re.Exec(req.Subject, pmatch, 0)
			}
		case revera.BenchReplace:
			op = func() { _, _ = re.ReplaceAll(req.Subject, req.Replacement, -1, 0) }
		default:
			panic("unknown bench kind " + req.Kind)
		}
		return revera.BenchAnswer(req.Name, op, req.Iters, req.Reps)
	}
	panic("unknown bench command " + f[0])
}
