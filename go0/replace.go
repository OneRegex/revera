package revera

import "strings"

// MatchAll calls fn for every non-overlapping match, left to right.
// The offsets in pmatch are absolute in subject.
// The slice has NumSub()+1 elements, and every call reuses it, so fn must copy what it keeps.
// A return of false from fn stops the scan early.
//
// The scan reports at most limit matches.
// A negative limit means no bound, like the preg_replace limit or the n of FindAll.
//
// Selection follows the classic global-substitution rule.
// After a match, the next search starts at its end, and it skips a null match that starts exactly there.
// A null match otherwise counts, and the scan then moves one character forward.
//
// An expression compiled with NoSub reports ENoSub, because iteration needs the match offsets.
func (re *Regexp) MatchAll(subject string, limit int, eflags ExecFlags, fn func(pmatch []Match) bool) error {
	if re.flags&NoSub != 0 {
		return &Error{Code: ENoSub, Pos: -1}
	}
	if limit == 0 {
		return nil
	}
	pmatch := make([]Match, re.nsub+1)
	pos := 0
	lastEnd := -1
	for pos <= len(subject) {
		flags := re.continuationFlags(subject, pos, eflags)
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
			limit--
			if limit == 0 {
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
		_, size := decodeRune(subject[so:])
		pos = so + size
	}
	return nil
}

// replPart is one piece of a parsed replacement text.
// A part with text is a literal.
// Every other part names a group, where 0 is the whole match.
type replPart struct {
	lit   string
	group int
}

// parseReplacement splits the sed-style replacement text into parts.
// An ampersand inserts the whole match, and a backslash-digit pair inserts one group.
// A backslash escapes the next character.
// A digit above nsub reports ESubReg, and a trailing backslash reports EEscape.
func parseReplacement(replacement string, nsub int) ([]replPart, error) {
	parts := make([]replPart, 0, 4)
	start := 0
	for idx := 0; idx < len(replacement); idx++ {
		c := replacement[idx]
		if c != '&' && c != '\\' {
			continue
		}
		if start < idx {
			parts = append(parts, replPart{lit: replacement[start:idx]})
		}
		if c == '&' {
			parts = append(parts, replPart{group: 0})
			start = idx + 1
			continue
		}
		if idx+1 == len(replacement) {
			return nil, compileError(EEscape, idx)
		}
		next := replacement[idx+1]
		if next >= '0' && next <= '9' {
			group := int(next - '0')
			if group == 0 || group > nsub {
				return nil, compileError(ESubReg, idx)
			}
			parts = append(parts, replPart{group: group})
		} else {
			parts = append(parts, replPart{lit: replacement[idx+1 : idx+2]})
		}
		idx++
		start = idx + 1
	}
	if start < len(replacement) {
		parts = append(parts, replPart{lit: replacement[start:]})
	}
	return parts, nil
}

// replaceAll walks every match with MatchAll and rebuilds the subject.
// The write callback emits the replacement for one match.
// A subject without any match comes back unchanged and without a copy.
func (re *Regexp) replaceAll(subject string, limit int, eflags ExecFlags,
	write func(out *strings.Builder, pmatch []Match)) (string, error) {
	var out strings.Builder
	last := 0
	any := false
	err := re.MatchAll(subject, limit, eflags, func(pmatch []Match) bool {
		if !any {
			// One reservation covers the common case, where the result stays near the subject size.
			out.Grow(len(subject) + len(subject)/8)
			any = true
		}
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

// ReplaceAll returns subject with every non-overlapping match replaced by replacement, like the sed s///g command.
// In replacement, & stands for the whole match and \1 through \9 for one group.
// A backslash escapes the next character, so \& and \\ are literal.
// A reference to a nonparticipating group inserts nothing, and a reference past NumSub() reports ESubReg.
// Match iteration follows the MatchAll rules, and limit bounds the replacement count the same way.
// The rest of the subject stays as it is.
//
// An expression compiled with NoSub reports ENoSub.
func (re *Regexp) ReplaceAll(subject, replacement string, limit int, eflags ExecFlags) (string, error) {
	parts, perr := parseReplacement(replacement, re.nsub)
	if perr != nil {
		return "", perr
	}
	return re.replaceAll(subject, limit, eflags, func(out *strings.Builder, pmatch []Match) {
		for _, part := range parts {
			if part.lit != "" {
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

// ReplaceAllFunc returns subject with every non-overlapping match replaced by the return value of repl.
// The callback receives the same pmatch slice as MatchAll: absolute offsets, reused between calls.
// Its result goes in as literal text, with no & or backslash expansion.
// Match iteration follows the MatchAll rules, and limit bounds the replacement count the same way.
//
// An expression compiled with NoSub reports ENoSub.
func (re *Regexp) ReplaceAllFunc(subject string, limit int, eflags ExecFlags,
	repl func(pmatch []Match) string) (string, error) {
	return re.replaceAll(subject, limit, eflags, func(out *strings.Builder, pmatch []Match) {
		out.WriteString(repl(pmatch))
	})
}
