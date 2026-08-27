package revera

// Host file, outside the Vego subset.
// It implements the reference side of the cross-language differential driver protocol.
// The Zig, C++ and Rust drivers implement the same line protocol.
// The crosscheck harness feeds every driver the same commands and compares the outputs against this implementation.
//
// Commands, one per line, strings hex-encoded with "-" for empty:
//
//	P                                  -> P 1
//	L <namehex> <collhex>              -> L <ok>
//	C <flags> <patternhex>             -> C <code> <pos> <nsub>
//	X <eflags> <subjecthex>            -> X <code> <matched> [so,eo ...]
//	R <limit> <eflags> <replhex> <subjecthex> -> R <code> <pos> <outhex>
//	I <limit> <eflags> <subjecthex>    -> I <code> <n> [row|row...]
//	T <maxinput>                       -> T <hasSolver> <heap> <stack> <steps>
//	O <lo> <hi>                        -> O <hash>

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

type DriverSession struct {
	base  Locale
	cur   Locale
	re    Regexp
	valid bool
}

func NewDriverSession() *DriverSession {
	s := &DriverSession{}
	s.base, _ = LocaleLoad(embeddedLocaleData)
	s.cur = LocalePOSIX()
	return s
}

func driverDecode(tok string) string {
	if tok == "-" {
		return ""
	}
	b, err := hex.DecodeString(tok)
	if err != nil {
		panic("bad hex token " + tok)
	}
	return string(b)
}

// DriverEncode hex-encodes a protocol string, with "-" standing for the empty string.
// crosscheck uses the same encoding when it builds command lines.
func DriverEncode(s string) string {
	if s == "" {
		return "-"
	}
	return hex.EncodeToString([]byte(s))
}

func driverInt(tok string) int {
	v, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		panic("bad integer token " + tok)
	}
	return int(v)
}

// Eval runs one protocol command and returns its output line.
func (s *DriverSession) Eval(line string) string {
	f := strings.Fields(line)
	switch f[0] {
	case "P":
		s.cur = LocalePOSIX()
		return "P 1"
	case "L":
		loc, ok := LocaleSelect(&s.base, driverDecode(f[1]), driverDecode(f[2]))
		if ok {
			s.cur = loc
		}
		return fmt.Sprintf("L %d", boolInt(ok))
	case "C":
		flags := uint32(driverInt(f[1]))
		re, err := Compile(driverDecode(f[2]), s.cur, flags)
		if err.Code != ErrNone {
			s.valid = false
			return fmt.Sprintf("C %d %d 0", err.Code, err.Pos)
		}
		s.re = re
		s.valid = true
		return fmt.Sprintf("C 0 0 %d", NumSub(&s.re))
	case "X":
		if !s.valid {
			return "X ERR"
		}
		eflags := uint32(driverInt(f[1]))
		subject := driverDecode(f[2])
		pmatch := make([]Match, NumSub(&s.re)+1)
		ok, err := Exec(&s.re, subject, pmatch, eflags)
		if err.Code != ErrNone {
			return fmt.Sprintf("X %d 0", err.Code)
		}
		if !ok {
			return "X 0 0"
		}
		var b strings.Builder
		b.WriteString("X 0 1")
		for _, m := range pmatch {
			fmt.Fprintf(&b, " %d,%d", m.So, m.Eo)
		}
		return b.String()
	case "R":
		if !s.valid {
			return "R ERR"
		}
		limit := driverInt(f[1])
		eflags := uint32(driverInt(f[2]))
		repl := driverDecode(f[3])
		subject := driverDecode(f[4])
		out, err := ReplaceAll(&s.re, subject, repl, limit, eflags)
		if err.Code != ErrNone {
			return fmt.Sprintf("R %d %d -", err.Code, err.Pos)
		}
		return "R 0 0 " + DriverEncode(out)
	case "I":
		if !s.valid {
			return "I ERR"
		}
		limit := driverInt(f[1])
		eflags := uint32(driverInt(f[2]))
		subject := driverDecode(f[3])
		it, err := MatchIterInit(&s.re, limit)
		if err.Code != ErrNone {
			return fmt.Sprintf("I %d 0", err.Code)
		}
		pmatch := make([]Match, NumSub(&s.re)+1)
		var rows []string
		for {
			ok, nerr := MatchIterNext(&s.re, &it, subject, eflags, pmatch)
			if nerr.Code != ErrNone {
				return fmt.Sprintf("I %d 0", nerr.Code)
			}
			if !ok {
				break
			}
			var parts []string
			for _, m := range pmatch {
				parts = append(parts, fmt.Sprintf("%d,%d", m.So, m.Eo))
			}
			rows = append(rows, strings.Join(parts, ","))
		}
		out := fmt.Sprintf("I 0 %d", len(rows))
		if len(rows) > 0 {
			out += " " + strings.Join(rows, "|")
		}
		return out
	case "T":
		if !s.valid {
			return "T ERR"
		}
		c := ContractFor(&s.re, driverInt(f[1]))
		return fmt.Sprintf("T %d %d %d %d", boolInt(c.HasSolver),
			ContractHeapBytes(&c), ContractStackBytes(&c), ContractSteps(&c))
	case "O":
		lo := int32(driverInt(f[1]))
		hi := int32(driverInt(f[2]))
		h := uint64(0xcbf29ce484222325)
		for r := lo; r < hi; r++ {
			h ^= uint64(uint32(localeToUpper(&s.cur, r)))
			h *= 0x100000001b3
			h ^= uint64(uint32(localeToLower(&s.cur, r)))
			h *= 0x100000001b3
		}
		return fmt.Sprintf("O %d", h)
	}
	panic("unknown driver command " + f[0])
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
