package revera

import (
	"strings"
	"testing"
)

func TestBenchCasesCompile(t *testing.T) {
	base := MustEmbeddedLocale()
	groups := map[string]bool{}
	for _, g := range BenchGroups {
		groups[g.Name] = true
	}
	seen := map[string]bool{}
	for _, c := range BenchCases() {
		key := c.Key()
		if seen[key] {
			t.Fatalf("duplicate case %s", key)
		}
		seen[key] = true
		if !groups[c.Group] {
			t.Fatalf("%s: group %q is not in BenchGroups", key, c.Group)
		}
		if c.Iters <= 0 {
			t.Fatalf("%s: iteration count %d", key, c.Iters)
		}
		loc, ok := LocaleByName(&base, c.Locale)
		if !ok {
			t.Fatalf("%s: locale %q missing", key, c.Locale)
		}
		if _, err := Compile(c.Pattern, loc, c.Flags); err.Code != ErrNone {
			t.Fatalf("%s: Compile(%q) code %d", key, c.Pattern, err.Code)
		}
	}
}

func TestBenchSessionProtocol(t *testing.T) {
	s := NewBenchSession()
	if got := s.Eval("P"); got != "P 1" {
		t.Fatalf("P answered %q", got)
	}
	if got := s.Eval("L " + DriverEncode("cs") + " -"); got != "L 1" {
		t.Fatalf("L cs answered %q", got)
	}
	if got := s.Eval("L " + DriverEncode("nosuchlocale") + " -"); got != "L 0" {
		t.Fatalf("L nosuchlocale answered %q", got)
	}
	c := BenchCase{Name: "t", Group: "g", Kind: BenchMatch, Pattern: "([[.ch.]]|c)h?", Subject: "chch", Iters: 5}
	line := BenchCommand(c, 3)
	if !strings.HasPrefix(line, "B g/t match 5 3 0 ") {
		t.Fatalf("BenchCommand gave %q", line)
	}
	r, err := ParseBenchResult(s.Eval(line))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "g/t" || r.Code != 0 || len(r.Nanos) != 3 || r.Allocs <= 0 || r.Bytes <= 0 {
		t.Fatalf("unexpected result %+v", r)
	}
	bad := BenchCase{Name: "e", Group: "g", Kind: BenchCompile, Pattern: "(", Iters: 5}
	r, err = ParseBenchResult(s.Eval(BenchCommand(bad, 3)))
	if err != nil {
		t.Fatal(err)
	}
	if r.Code == 0 || len(r.Nanos) != 0 || r.Bytes != 0 || r.Allocs != 0 {
		t.Fatalf("a failed compile must answer the code alone: %+v", r)
	}
	if _, err := ParseBenchResult("B x"); err == nil {
		t.Fatal("a short answer must fail to parse")
	}
	if _, err := ParseBenchResult("B x 0 1 1 nan"); err == nil {
		t.Fatal("a non-numeric timing must fail to parse")
	}
}
