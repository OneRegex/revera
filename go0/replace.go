package revera

import (
	"strings"
	"unicode/utf8"
)

// MatchAll calls fn for every non-overlapping match, left to right.
// Offsets in pmatch are absolute in subject. The slice has NumSub()+1
// elements and is reused between calls, so fn must copy what it keeps.
// A return of false from fn stops the scan early.
//
// The scan reports at most limit matches. A negative limit means no
// bound, like the preg_replace limit or the n of FindAll.
//
// Selection follows the classic global-substitution rule: after a
// match, the next search starts at its end, and a null match that
// starts exactly there is skipped. A null match otherwise counts and
// the scan then moves one character forward.
//
// The expression must not be compiled with NoSub, because iteration
// needs the match offsets.
func (re *Regexp) MatchAll(subject string, limit int, eflags ExecFlags, fn func(pmatch []Match) bool) error {
	if re.flags&NoSub != 0 {
		return compileError(BadPat, -1)
	}
	if limit == 0 {
		return nil
	}
	count := 0
	pmatch := make([]Match, re.nsub+1)
	pos := 0
	lastEnd := -1
	for pos <= len(subject) {
		flags := eflags
		if pos > 0 {
			// Only the true start keeps the caller's NotBOL. Later
			// positions have a line boundary only after a newline,
			// and only when the Newline flag keeps ^ alive there.
			flags &^= NotBOL
			if re.flags&Newline == 0 || subject[pos-1] != '\n' {
				flags |= NotBOL
			}
		}
		matched, err := re.Exec(subject[pos:], pmatch, flags)
		if err != nil {
			return err
		}
		if !matched {
			return nil
		}
		for idx := range pmatch {
			if pmatch[idx].So >= 0 {
				pmatch[idx].So += pos
				pmatch[idx].Eo += pos
			}
		}
		so, eo := pmatch[0].So, pmatch[0].Eo
		if so != eo || so != lastEnd {
			if !fn(pmatch) {
				return nil
			}
			lastEnd = eo
			count++
			if count == limit {
				return nil
			}
		}
		if eo > so {
			pos = eo
			continue
		}
		if so >= len(subject) {
			return nil
		}
		_, size := utf8.DecodeRuneInString(subject[so:])
		pos = so + size
	}
	return nil
}

// replPart is one piece of a parsed replacement text: either a literal
// span, or a reference to a group. Group 0 is the whole match.
type replPart struct {
	lit   string
	group int
}

// parseReplacement splits the sed-style replacement text into parts.
// An ampersand inserts the whole match and a backslash-digit pair
// inserts one group. Backslash escapes the next character. A digit
// above nsub reports ESubReg; a trailing backslash reports EEscape.
func parseReplacement(replacement string, nsub int) ([]replPart, error) {
	var parts []replPart
	start := 0
	for idx := 0; idx < len(replacement); idx++ {
		switch replacement[idx] {
		case '&':
			if start < idx {
				parts = append(parts, replPart{lit: replacement[start:idx], group: -1})
			}
			parts = append(parts, replPart{group: 0})
			start = idx + 1
		case '\\':
			if idx+1 == len(replacement) {
				return nil, compileError(EEscape, idx)
			}
			if start < idx {
				parts = append(parts, replPart{lit: replacement[start:idx], group: -1})
			}
			next := replacement[idx+1]
			if next >= '0' && next <= '9' {
				group := int(next - '0')
				if group == 0 || group > nsub {
					return nil, compileError(ESubReg, idx)
				}
				parts = append(parts, replPart{group: group})
			} else {
				parts = append(parts, replPart{lit: replacement[idx+1 : idx+2], group: -1})
			}
			idx++
			start = idx + 1
		}
	}
	if start < len(replacement) {
		parts = append(parts, replPart{lit: replacement[start:], group: -1})
	}
	return parts, nil
}

// replaceAll walks every match with MatchAll and rebuilds the subject.
// The write callback emits the replacement for one match. A subject
// without any match comes back unchanged and without a copy.
func (re *Regexp) replaceAll(subject string, limit int, eflags ExecFlags,
	write func(out *strings.Builder, pmatch []Match)) (string, error) {
	var out strings.Builder
	last := 0
	any := false
	err := re.MatchAll(subject, limit, eflags, func(pmatch []Match) bool {
		any = true
		out.WriteString(subject[last:pmatch[0].So])
		write(&out, pmatch)
		last = pmatch[0].Eo
		return true
	})
	if err != nil {
		return "", err
	}
	if !any {
		return subject, nil
	}
	out.WriteString(subject[last:])
	return out.String(), nil
}

// ReplaceAll returns subject with every non-overlapping match replaced
// by replacement, like the sed s///g command. In replacement, & stands
// for the whole match and \1 through \9 for one group. Backslash
// escapes the next character, so \& and \\ are literal. A reference to
// a nonparticipating group inserts nothing; a reference past NumSub()
// reports ESubReg. Match iteration follows the MatchAll rules, and
// limit bounds the replacement count the same way; the rest of the
// subject stays as it is.
//
// The expression must not be compiled with NoSub.
func (re *Regexp) ReplaceAll(subject, replacement string, limit int, eflags ExecFlags) (string, error) {
	parts, perr := parseReplacement(replacement, re.nsub)
	if perr != nil {
		return "", perr
	}
	return re.replaceAll(subject, limit, eflags, func(out *strings.Builder, pmatch []Match) {
		for _, part := range parts {
			if part.group < 0 {
				out.WriteString(part.lit)
				continue
			}
			ref := pmatch[part.group]
			if ref.So >= 0 {
				out.WriteString(subject[ref.So:ref.Eo])
			}
		}
	})
}

// ReplaceAllFunc returns subject with every non-overlapping match
// replaced by the return value of repl. The callback receives the
// same pmatch slice as MatchAll: absolute offsets, reused between
// calls. Its result is inserted literally, with no & or backslash
// expansion. Match iteration follows the MatchAll rules, and limit
// bounds the replacement count the same way.
//
// The expression must not be compiled with NoSub.
func (re *Regexp) ReplaceAllFunc(subject string, limit int, eflags ExecFlags,
	repl func(pmatch []Match) string) (string, error) {
	return re.replaceAll(subject, limit, eflags, func(out *strings.Builder, pmatch []Match) {
		out.WriteString(repl(pmatch))
	})
}
