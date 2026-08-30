package revera

import (
	"slices"
	"strings"
	"testing"
)

func matchAllSpans(t *testing.T, re *Regexp, subject string, eflags ExecFlags) []Match {
	t.Helper()
	var spans []Match
	err := re.MatchAll(subject, -1, eflags, func(pmatch []Match) bool {
		spans = append(spans, pmatch[0])
		return true
	})
	if err != nil {
		t.Fatalf("MatchAll(%q) failed: %v", subject, err)
	}
	return spans
}

func TestMatchAllSpans(t *testing.T) {
	cases := []struct {
		pattern string
		subject string
		want    []Match
	}{
		{"ab", "abxabab", []Match{{0, 2}, {3, 5}, {5, 7}}},
		{"a+", "baaacada", []Match{{1, 4}, {5, 6}, {7, 8}}},
		// A null match right after a real match is skipped, like sed.
		{"a*", "abc", []Match{{0, 1}, {2, 2}, {3, 3}}},
		{"x*", "", []Match{{0, 0}}},
		{"q", "abc", nil},
		// The candidates overlap, and the scan restarts at the match end.
		{"aba", "ababa", []Match{{0, 3}}},
	}
	for _, tc := range cases {
		re := compileOK(t, tc.pattern, 0)
		got := matchAllSpans(t, re, tc.subject, 0)
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%q on %q: got %v, want %v", tc.pattern, tc.subject, got, tc.want)
		}
	}
}

func TestMatchAllGroups(t *testing.T) {
	re := compileOK(t, "(a+)(b?)", 0)
	var all [][]Match
	err := re.MatchAll("aab a", -1, 0, func(pmatch []Match) bool {
		all = append(all, slices.Clone(pmatch))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]Match{
		{{0, 3}, {0, 2}, {2, 3}},
		{{4, 5}, {4, 5}, {5, 5}},
	}
	if !slices.EqualFunc(all, want, slices.Equal) {
		t.Fatalf("got %v, want %v", all, want)
	}
}

func TestMatchAllEarlyStop(t *testing.T) {
	re := compileOK(t, "a", 0)
	count := 0
	err := re.MatchAll("aaaa", -1, 0, func(pmatch []Match) bool {
		count++
		return count < 2
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("callback ran %d times, want 2", count)
	}
}

func TestMatchAllAnchors(t *testing.T) {
	// Without the Newline flag, ^ holds only at the true start.
	re := compileOK(t, "^a", 0)
	got := matchAllSpans(t, re, "aaa", 0)
	if !slices.Equal(got, []Match{{0, 1}}) {
		t.Fatalf("^a spans = %v", got)
	}

	// With the Newline flag, ^ also holds after each newline.
	re = compileOK(t, "^a", Newline)
	got = matchAllSpans(t, re, "ab\nab\na", 0)
	if !slices.Equal(got, []Match{{0, 1}, {3, 4}, {6, 7}}) {
		t.Fatalf("newline ^a spans = %v", got)
	}

	// NotBOL applies to the first position only.
	got = matchAllSpans(t, re, "a\na", NotBOL)
	if !slices.Equal(got, []Match{{2, 3}}) {
		t.Fatalf("NotBOL ^a spans = %v", got)
	}
}

func TestMatchAllMultibyteAdvance(t *testing.T) {
	// Null matches must advance one character, not one byte.
	re := compileOK(t, "x*", 0)
	got := matchAllSpans(t, re, "é", 0)
	if !slices.Equal(got, []Match{{0, 0}, {2, 2}}) {
		t.Fatalf("multibyte spans = %v", got)
	}
}

func TestMatchAllNoSub(t *testing.T) {
	re := compileOK(t, "a", NoSub)
	err := re.MatchAll("a", -1, 0, func(pmatch []Match) bool { return true })
	if e, ok := err.(*Error); !ok || e.Code != ENoSub {
		t.Fatalf("NoSub error = %v", err)
	}
}

func TestMatchAllLimit(t *testing.T) {
	re := compileOK(t, "a", 0)
	count := 0
	err := re.MatchAll("aaaa", 2, 0, func(pmatch []Match) bool {
		count++
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("limit 2 gave %d matches", count)
	}

	count = 0
	if err := re.MatchAll("aaaa", 0, 0, func(pmatch []Match) bool {
		count++
		return true
	}); err != nil || count != 0 {
		t.Fatalf("limit 0 gave %d matches, err %v", count, err)
	}

	// A skipped null match does not count against the limit.
	re = compileOK(t, "a*", 0)
	var spans []Match
	err = re.MatchAll("abc", 2, 0, func(pmatch []Match) bool {
		spans = append(spans, pmatch[0])
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spans, []Match{{0, 1}, {2, 2}}) {
		t.Fatalf("limited spans = %v", spans)
	}
}

func TestReplaceAllLimit(t *testing.T) {
	re := compileOK(t, "a", 0)
	got, err := re.ReplaceAll("aaaa", "-", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "--aa" {
		t.Fatalf("limit 2 = %q", got)
	}
	got, err = re.ReplaceAll("aaaa", "-", 0, 0)
	if err != nil || got != "aaaa" {
		t.Fatalf("limit 0 = %q, err %v", got, err)
	}
	got, err = re.ReplaceAllFunc("aaaa", 1, 0, func(pmatch []Match) string {
		return "-"
	})
	if err != nil || got != "-aaa" {
		t.Fatalf("func limit 1 = %q, err %v", got, err)
	}
}

func TestReplaceAll(t *testing.T) {
	cases := []struct {
		pattern     string
		subject     string
		replacement string
		want        string
	}{
		{"ab", "abxab", "-", "-x-"},
		{"a*", "abc", "-", "-b-c-"},
		{"a+", "baaac", "[&]", "b[aaa]c"},
		{"(a+)(b+)", "aabb xab", `\2\1`, "bbaa xba"},
		{"(a)|(b)", "ab", `[\1\2]`, "[a][b]"},
		{`\.`, "a.b", `\&`, "a&b"},
		{"a", "a", `\\`, `\`},
		{"a", "a", `x\ny`, "xny"},
		{"q", "abc", "-", "abc"},
		{"x*", "ab", "-", "-a-b-"},
	}
	for _, tc := range cases {
		re := compileOK(t, tc.pattern, 0)
		got, err := re.ReplaceAll(tc.subject, tc.replacement, -1, 0)
		if err != nil {
			t.Fatalf("ReplaceAll(%q, %q, %q) failed: %v",
				tc.pattern, tc.subject, tc.replacement, err)
		}
		if got != tc.want {
			t.Fatalf("ReplaceAll(%q, %q, %q) = %q, want %q",
				tc.pattern, tc.subject, tc.replacement, got, tc.want)
		}
	}
}

func TestReplaceAllFunc(t *testing.T) {
	re := compileOK(t, "a+", 0)
	subject := "baaac ad"
	got, err := re.ReplaceAllFunc(subject, -1, 0, func(pmatch []Match) string {
		return strings.ToUpper(subject[pmatch[0].So:pmatch[0].Eo])
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "bAAAc Ad" {
		t.Fatalf("ReplaceAllFunc = %q", got)
	}

	// The callback result is literal text, with no & or backslash expansion.
	got, err = re.ReplaceAllFunc("a", -1, 0, func(pmatch []Match) string {
		return `&\1`
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `&\1` {
		t.Fatalf("literal result = %q", got)
	}

	// Groups are visible through pmatch.
	re = compileOK(t, "(a+)(b+)", 0)
	subject = "aabb xab"
	got, err = re.ReplaceAllFunc(subject, -1, 0, func(pmatch []Match) string {
		return subject[pmatch[2].So:pmatch[2].Eo] + subject[pmatch[1].So:pmatch[1].Eo]
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "bbaa xba" {
		t.Fatalf("group result = %q", got)
	}

	re = compileOK(t, "a", NoSub)
	if _, err := re.ReplaceAllFunc("a", -1, 0, func(pmatch []Match) string { return "" }); err == nil {
		t.Fatal("ReplaceAllFunc accepted a NoSub expression")
	}
}

func TestReplaceAllFuncCallbackMutation(t *testing.T) {
	re := compileOK(t, "a", 0)
	got, err := re.ReplaceAllFunc("a", -1, 0, func(pmatch []Match) string {
		pmatch[0] = Match{So: 2, Eo: 2}
		return "x"
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "x" {
		t.Fatalf("ReplaceAllFunc = %q", got)
	}
}

func TestReplaceAllErrors(t *testing.T) {
	re := compileOK(t, "(a)", 0)
	if _, err := re.ReplaceAll("a", `\2`, -1, 0); err == nil {
		t.Fatal("accepted a reference past NumSub")
	} else if e, ok := err.(*Error); !ok || e.Code != ESubReg {
		t.Fatalf("reference error = %v", err)
	}
	if _, err := re.ReplaceAll("a", `\0`, -1, 0); err == nil {
		t.Fatal(`accepted \0`)
	}
	if _, err := re.ReplaceAll("a", `x\`, -1, 0); err == nil {
		t.Fatal("accepted a trailing backslash")
	} else if e, ok := err.(*Error); !ok || e.Code != EEscape {
		t.Fatalf("trailing backslash error = %v", err)
	}
	re = compileOK(t, "a", NoSub)
	for _, replacement := range []string{"-", `\`, `\1`} {
		_, err := re.ReplaceAll("a", replacement, -1, 0)
		if e, ok := err.(*Error); !ok || e.Code != ENoSub {
			t.Fatalf("NoSub replacement %q error = %v", replacement, err)
		}
	}
}
