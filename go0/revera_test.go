package revera

import (
	"testing"

	"revera/locale"
)

func compileOK(t *testing.T, pattern string, flags CompileFlags) *Regexp {
	t.Helper()
	re, err := Compile(pattern, locale.POSIX(), flags)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}
	return re
}

func execCaps(t *testing.T, re *Regexp, subject string, eflags ExecFlags) []Match {
	t.Helper()
	pmatch := make([]Match, re.NumSub()+1)
	ok, err := re.Exec(subject, pmatch, eflags)
	if err != nil {
		t.Fatalf("Exec(%q) failed: %v", subject, err)
	}
	if !ok {
		return nil
	}
	return pmatch
}

type matchCase struct {
	pattern string
	subject string
	cflags  CompileFlags
	eflags  ExecFlags
	// want lists expected pmatch contents; nil means no match.
	want []Match
}

func runMatchCases(t *testing.T, cases []matchCase) {
	t.Helper()
	for _, c := range cases {
		re, err := Compile(c.pattern, locale.POSIX(), c.cflags)
		if err != nil {
			t.Errorf("Compile(%q) failed: %v", c.pattern, err)
			continue
		}
		pmatch := make([]Match, re.NumSub()+1)
		ok, err := re.Exec(c.subject, pmatch, c.eflags)
		if err != nil {
			t.Errorf("Exec(%q, %q) failed: %v", c.pattern, c.subject, err)
			continue
		}
		if c.want == nil {
			if ok {
				t.Errorf("%q on %q: matched %v, want no match",
					c.pattern, c.subject, pmatch)
			}
			continue
		}
		if !ok {
			t.Errorf("%q on %q: no match, want %v", c.pattern, c.subject, c.want)
			continue
		}
		for idx := range c.want {
			if pmatch[idx] != c.want[idx] {
				t.Errorf("%q on %q: pmatch[%d] = %v, want %v",
					c.pattern, c.subject, idx, pmatch[idx], c.want[idx])
			}
		}
	}
}

func TestSelectionAndGrouping(t *testing.T) {
	runMatchCases(t, []matchCase{
		{pattern: "b|ab", subject: "ab", want: []Match{{0, 2}}},
		{pattern: "a|ab", subject: "ab", want: []Match{{0, 2}}},
		{pattern: "(a|aa)(a*)", subject: "aaa",
			want: []Match{{0, 3}, {0, 2}, {2, 3}}},
		{pattern: "(ab)*", subject: "abab",
			want: []Match{{0, 4}, {2, 4}}},
		{pattern: "(a)?b", subject: "b",
			want: []Match{{0, 1}, {-1, -1}}},
		{pattern: "(a*)b", subject: "b",
			want: []Match{{0, 1}, {0, 0}}},
		{pattern: ".*c", subject: "abc abc", want: []Match{{0, 7}}},
		{pattern: ".*?c", subject: "abc abc", want: []Match{{0, 3}}},
		{pattern: "(.*?).*", subject: "abcdef",
			want: []Match{{0, 6}, {0, 0}}},
		{pattern: "(a?)(ab)?", subject: "ab",
			want: []Match{{0, 2}, {0, 0}, {0, 2}}},
		{pattern: ".*", subject: "abc", cflags: Minimal,
			want: []Match{{0, 0}}},
		{pattern: ".*?", subject: "abc", cflags: Minimal,
			want: []Match{{0, 3}}},
	})
}

func TestMinimalRepetition(t *testing.T) {
	runMatchCases(t, []matchCase{
		{pattern: "a{1,3}?", subject: "aaa", want: []Match{{0, 1}}},
		{pattern: "a{1,3}", subject: "aaa", cflags: Minimal,
			want: []Match{{0, 1}}},
		{pattern: "a{1,3}?", subject: "aaa", cflags: Minimal,
			want: []Match{{0, 3}}},
		{pattern: "a{2}?", subject: "aaa", want: []Match{{0, 2}}},
		{pattern: "(a*?)(a*)", subject: "aa",
			want: []Match{{0, 2}, {0, 0}, {0, 2}}},
		{pattern: "(a*)(a*?)", subject: "aa",
			want: []Match{{0, 2}, {0, 2}, {2, 2}}},
		{pattern: "a.*?b", subject: "a xx b yy b",
			want: []Match{{0, 6}}},
		{pattern: "a.*b", subject: "a xx b yy b",
			want: []Match{{0, 11}}},
		{pattern: "a+?", subject: "aaa", want: []Match{{0, 1}}},
		{pattern: "(a|b)*?c", subject: "abac",
			want: []Match{{0, 4}, {2, 3}}},
	})
}

func TestRepetitionAndCaptures(t *testing.T) {
	runMatchCases(t, []matchCase{
		{pattern: "(a*)+", subject: "aa", want: []Match{{0, 2}, {0, 2}}},
		{pattern: "(a*)+", subject: "", want: []Match{{0, 0}, {0, 0}}},
		// A null repetition takes one null occurrence when the operand
		// has one; null beats nonparticipation (sections 8.5 and 4.3).
		{pattern: "(a?)*", subject: "", want: []Match{{0, 0}, {0, 0}}},
		{pattern: "(a*)*", subject: "b", want: []Match{{0, 0}, {0, 0}}},
		{pattern: "(a)*", subject: "b", want: []Match{{0, 0}, {-1, -1}}},
		{pattern: "(a?){0,3}", subject: "", want: []Match{{0, 0}, {0, 0}}},
		{pattern: "((a?)?)*", subject: "",
			want: []Match{{0, 0}, {0, 0}, {0, 0}}},
		{pattern: "c((c)*)?", subject: "caab",
			want: []Match{{0, 1}, {1, 1}, {-1, -1}}},
		{pattern: "((..)?)*", subject: "a",
			want: []Match{{0, 0}, {0, 0}, {-1, -1}}},
		{pattern: "((..)?)*", subject: "aa",
			want: []Match{{0, 2}, {0, 2}, {0, 2}}},
		// Rule 1 of section 4.3: a shorter earlier match beats a longer
		// later match. BSD libc skips the leftmost empty match here.
		{pattern: "((c){0,2}$)?", subject: "caac",
			want: []Match{{0, 0}, {-1, -1}, {-1, -1}}},
		{pattern: "(([^a-c])*$)?", subject: "bc",
			want: []Match{{0, 0}, {-1, -1}, {-1, -1}}},
		{pattern: "((a)|b)*", subject: "ab",
			want: []Match{{0, 2}, {1, 2}, {-1, -1}}},
		{pattern: "(a?){2,}", subject: "",
			want: []Match{{0, 0}, {0, 0}}},
		{pattern: "(a|aa){1,2}", subject: "aaa",
			want: []Match{{0, 3}, {2, 3}}},
		{pattern: "a{2,3}", subject: "aaaa", want: []Match{{0, 3}}},
		{pattern: "a{0}", subject: "aaa", want: []Match{{0, 0}}},
		{pattern: "(ab){2}", subject: "abab",
			want: []Match{{0, 4}, {2, 4}}},
	})
}

func TestAnchorsAndNewline(t *testing.T) {
	runMatchCases(t, []matchCase{
		{pattern: "a^", subject: "a", want: nil},
		{pattern: "$a", subject: "a", want: nil},
		{pattern: ".", subject: "\n", want: []Match{{0, 1}}},
		{pattern: ".", subject: "\n", cflags: Newline, want: nil},
		{pattern: "[\n]", subject: "\n", cflags: Newline,
			want: []Match{{0, 1}}},
		{pattern: "[^a]", subject: "\n", cflags: Newline, want: nil},
		{pattern: "^b", subject: "a\nb", cflags: Newline, eflags: NotBOL,
			want: []Match{{2, 3}}},
		{pattern: "a$", subject: "a\nb", cflags: Newline, eflags: NotEOL,
			want: []Match{{0, 1}}},
		{pattern: "^a", subject: "a", eflags: NotBOL, want: nil},
		{pattern: "a$", subject: "a", eflags: NotEOL, want: nil},
		{pattern: "^", subject: "ab", want: []Match{{0, 0}}},
		{pattern: "$", subject: "ab", want: []Match{{2, 2}}},
	})
}

func TestBracketsPOSIXLocale(t *testing.T) {
	runMatchCases(t, []matchCase{
		{pattern: "[[.a.]]", subject: "a", want: []Match{{0, 1}}},
		{pattern: "[[=a=]]", subject: "a", want: []Match{{0, 1}}},
		{pattern: "[[=a=]]", subject: "b", want: nil},
		{pattern: "[^a]", subject: "A", cflags: ICase,
			want: []Match{{0, 1}}},
		{pattern: "[^a]", subject: "a", cflags: ICase,
			want: []Match{{0, 1}}},
		{pattern: "[[:digit:]]", subject: "7", want: []Match{{0, 1}}},
		{pattern: "[[:digit:]]", subject: "x", want: nil},
		{pattern: "[^[:digit:]]", subject: "A", want: []Match{{0, 1}}},
		{pattern: "[^[:digit:]]", subject: "7", want: nil},
		{pattern: "[a-c]", subject: "b", want: []Match{{0, 1}}},
		{pattern: "[a-c]", subject: "d", want: nil},
		{pattern: "[]-]", subject: "]", want: []Match{{0, 1}}},
		{pattern: "[]-]", subject: "-", want: []Match{{0, 1}}},
		{pattern: "[\\a]", subject: "\\", want: []Match{{0, 1}}},
		{pattern: "[\\a]", subject: "a", want: []Match{{0, 1}}},
		{pattern: "[]a]", subject: "]", want: []Match{{0, 1}}},
		{pattern: "[^]a]", subject: "b", want: []Match{{0, 1}}},
		{pattern: "[^]a]", subject: "]", want: nil},
		{pattern: "[-a]", subject: "-", want: []Match{{0, 1}}},
		{pattern: "[a-]", subject: "-", want: []Match{{0, 1}}},
		{pattern: "[--/]", subject: ".", want: []Match{{0, 1}}},
		{pattern: "[[.-.]-0]", subject: "/", want: []Match{{0, 1}}},
	})
}

func TestOrdinarySpecials(t *testing.T) {
	runMatchCases(t, []matchCase{
		{pattern: ")", subject: ")", want: []Match{{0, 1}}},
		{pattern: "a)b", subject: "a)b", want: []Match{{0, 3}}},
		{pattern: "]", subject: "]", want: []Match{{0, 1}}},
		{pattern: "}", subject: "}", want: []Match{{0, 1}}},
		{pattern: "\\}", subject: "}", want: []Match{{0, 1}}},
		{pattern: "\\]", subject: "]", want: []Match{{0, 1}}},
		{pattern: "\\.", subject: ".", want: []Match{{0, 1}}},
		{pattern: "\\.", subject: "a", want: nil},
		{pattern: "(a))", subject: "a)", want: []Match{{0, 2}, {0, 1}}},
	})
}

func TestCompileErrors(t *testing.T) {
	cases := []struct {
		pattern string
		code    Code
	}{
		{"", BadPat},
		{"()", BadPat},
		{"a|", BadPat},
		{"|a", BadPat},
		{"a||b", BadPat},
		{"(", BadPat}, // empty group content is found first
		{"(a", EParen},
		{"[a", EBrack},
		{"[", EBrack},
		{"a\\", EEscape},
		{"\\d", BadPat},
		{"*a", BadRpt},
		{"^*", BadRpt},
		{"a**", BadRpt},
		{"a+*", BadRpt},
		{"a??*", BadRpt},
		{"a{2}{3}", BadRpt},
		{"a{", EBrace},
		{"a{2", EBrace},
		{"a{x}", BadBR},
		{"a{2,1}", BadBR},
		{"a{256}", BadBR},
		{"a{1,256}", BadBR},
		{"a{,2}", BadBR},
		{"[[:foo:]]", ECType},
		{"[[.ch.]]", ECollate},
		{"[[=xy=]]", ECollate},
		{"[z-a]", ERange},
		{"[a-m-o]", ERange},
		{"[[:alpha:]-z]", ERange},
		{"[a-[:alpha:]]", ERange},
		{"[[=a=]-z]", ERange},
	}
	for _, c := range cases {
		_, err := Compile(c.pattern, locale.POSIX(), 0)
		if err == nil {
			t.Errorf("Compile(%q) succeeded, want %v", c.pattern, c.code)
			continue
		}
		if got := err.(*Error).Code; got != c.code {
			t.Errorf("Compile(%q) = %v, want %v", c.pattern, got, c.code)
		}
	}
}

func TestIntervalMax(t *testing.T) {
	re := compileOK(t, "a{255}", 0)
	subject := ""
	for range 255 {
		subject += "a"
	}
	caps := execCaps(t, re, subject, 0)
	if caps == nil || caps[0] != (Match{0, 255}) {
		t.Fatalf("a{255} = %v", caps)
	}
	if got := execCaps(t, re, subject[:254], 0); got != nil {
		t.Fatalf("a{255} on 254 a's matched %v", got)
	}
}

func TestAPIReporting(t *testing.T) {
	re := compileOK(t, "(a)(b)", 0)
	if re.NumSub() != 2 {
		t.Fatalf("NumSub = %d", re.NumSub())
	}
	pmatch := make([]Match, 4)
	ok, err := re.Exec("ab", pmatch, 0)
	if err != nil || !ok {
		t.Fatalf("Exec failed: %v %v", ok, err)
	}
	want := []Match{{0, 2}, {0, 1}, {1, 2}, {-1, -1}}
	for idx := range want {
		if pmatch[idx] != want[idx] {
			t.Errorf("pmatch[%d] = %v, want %v", idx, pmatch[idx], want[idx])
		}
	}

	// Multibyte offsets are bytes; preference counts characters.
	re = compileOK(t, ".", 0)
	caps := execCaps(t, re, "é", 0)
	if caps[0] != (Match{0, 2}) {
		t.Fatalf("dot on é = %v", caps)
	}

	// NoSub must leave pmatch untouched.
	re = compileOK(t, "ab", NoSub)
	sentinel := []Match{{7, 7}}
	ok, err = re.Exec("ab", sentinel, 0)
	if err != nil || !ok {
		t.Fatalf("NoSub Exec failed: %v %v", ok, err)
	}
	if sentinel[0] != (Match{7, 7}) {
		t.Fatalf("NoSub wrote pmatch: %v", sentinel)
	}
	ok, err = re.Exec("ab", nil, 0)
	if err != nil || !ok {
		t.Fatalf("NoSub nil pmatch failed: %v %v", ok, err)
	}
}

func TestICaseLiterals(t *testing.T) {
	runMatchCases(t, []matchCase{
		{pattern: "abc", subject: "AbC", cflags: ICase,
			want: []Match{{0, 3}}},
		{pattern: "ABC", subject: "abc", cflags: ICase,
			want: []Match{{0, 3}}},
		{pattern: "abc", subject: "AbC", want: nil},
		{pattern: "[a-c]", subject: "B", cflags: ICase,
			want: []Match{{0, 1}}},
		{pattern: "[[:lower:]]", subject: "A", cflags: ICase,
			want: []Match{{0, 1}}},
	})
}

func TestCollatingElementMatch(t *testing.T) {
	cs, ok := locale.Open("cs", "")
	if !ok {
		t.Fatal("cs locale missing")
	}
	re, err := Compile("[[.ch.]]", cs, 0)
	if err != nil {
		t.Fatalf("cs [[.ch.]] failed: %v", err)
	}
	pmatch := make([]Match, 1)
	matched, err := re.Exec("chleba", pmatch, 0)
	if err != nil || !matched {
		t.Fatalf("cs [[.ch.]] on chleba: %v %v", matched, err)
	}
	if pmatch[0] != (Match{0, 2}) {
		t.Fatalf("cs [[.ch.]] consumed %v, want [0,2)", pmatch[0])
	}

	// The same bracket must still accept a plain c or h list member.
	re, err = Compile("[ch]x", cs, 0)
	if err != nil {
		t.Fatalf("cs [ch]x failed: %v", err)
	}
	matched, err = re.Exec("cx", pmatch, 0)
	if err != nil || !matched {
		t.Fatalf("cs [ch]x on cx: %v %v", matched, err)
	}
}

func TestInvalidUTF8Subject(t *testing.T) {
	re := compileOK(t, ".", 0)
	pmatch := make([]Match, 1)
	ok, err := re.Exec("\xff", pmatch, 0)
	if err != nil {
		t.Fatalf("invalid subject error: %v", err)
	}
	if ok {
		t.Fatal("dot matched an invalid byte")
	}
	// A valid character after the invalid byte still matches.
	ok, err = re.Exec("\xffa", pmatch, 0)
	if err != nil || !ok {
		t.Fatalf("match after invalid byte: %v %v", ok, err)
	}
	if pmatch[0] != (Match{1, 2}) {
		t.Fatalf("match after invalid byte = %v", pmatch[0])
	}
}

func TestNULIsOrdinary(t *testing.T) {
	re := compileOK(t, "a\x00b", 0)
	pmatch := make([]Match, 1)
	ok, err := re.Exec("a\x00b", pmatch, 0)
	if err != nil || !ok {
		t.Fatalf("NUL literal: %v %v", ok, err)
	}
	re = compileOK(t, ".", 0)
	ok, err = re.Exec("\x00", pmatch, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("dot matched NUL")
	}
}
