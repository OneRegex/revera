package revera

import "testing"

func TestContractOmitsUnreachableCaptureBackends(t *testing.T) {
	re, err := Compile("a*", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile failed: %v", err)
	}
	c := ContractFor(&re, 64)
	if c.HasOnePass || c.HasSolver {
		t.Fatalf("zero-group contract reports phase B: %+v", c)
	}
	if ContractHeapBytes(&c) != c.Matcher.HeapBytes ||
		ContractStackBytes(&c) != c.Matcher.StackBytes ||
		ContractSteps(&c) != c.Matcher.Steps {
		t.Fatalf("zero-group contract is not matcher-only: %+v", c)
	}

	grouped, err := Compile("(a*)", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile grouped expression failed: %v", err)
	}
	groupedContract := ContractFor(&grouped, 64)
	if !groupedContract.HasOnePass || !groupedContract.HasSolver {
		t.Fatalf("grouped contract omitted phase B: %+v", groupedContract)
	}
}

// The POSIX bracket figures are fixed.
// A full locale's depend on its tables, so only their order is checked.
func TestBracketAtomCost(t *testing.T) {
	posix := LocalePOSIX()
	czech, ok := LocaleOpen(EmbeddedLocaleData(), "cs", "")
	if !ok {
		t.Fatal("LocaleOpen cs failed")
	}
	atom := func(pattern string, loc Locale, flags uint32) int64 {
		re, err := Compile(pattern, loc, flags)
		if err.Code != ErrNone {
			t.Fatalf("Compile(%q) failed: %v", pattern, err)
		}
		return atomCost(&re)
	}
	fixed := []struct {
		pattern string
		flags   uint32
		want    int64
	}{
		{"[a-z]", 0, 4},
		{"[a-z]", FlagICase, 10},
		{"[a-zA-Z0-9_]", 0, 6},
		{"[[:alpha:]]", 0, 6},
		{"[^a-z]", 0, 4},
		{"[[=a=]]", 0, 13},
		{"[[=a=]]", FlagICase, 28},
	}
	for _, tc := range fixed {
		if got := atom(tc.pattern, posix, tc.flags); got != tc.want {
			t.Errorf("atomCost(%q, POSIX, %d) = %d, want %d", tc.pattern, tc.flags, got, tc.want)
		}
	}
	if got := atom("[[.ch.]]", czech, 0); got != 15 {
		t.Errorf("atomCost([[.ch.]], cs) = %d, want 15", got)
	}
	plain := atom("[[=a=]]", czech, 0)
	if plain <= atom("[[=a=]]", posix, 0) || plain < 1000 {
		t.Errorf("atomCost([[=a=]], cs) = %d, expected the locale searches to show", plain)
	}
	if folded := atom("[[=a=]]", czech, FlagICase); folded <= plain {
		t.Errorf("atomCost([[=a=]], cs, ICase) = %d, not above %d", folded, plain)
	}
	// Every added equivalence class costs the same primary comparison.
	two := atom("[[=a=][=b=]]", czech, 0)
	three := atom("[[=a=][=b=][=c=]]", czech, 0)
	if two-plain != three-two {
		t.Errorf("equivalence classes cost %d then %d", two-plain, three-two)
	}
}
