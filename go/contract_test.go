package revera

import (
	"runtime"
	"strings"
	"testing"
)

func TestContractSelectsReachableBackends(t *testing.T) {
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

	grouped, err := Compile("(abc+)", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile grouped expression failed: %v", err)
	}
	groupedContract := ContractFor(&grouped, 1000)
	if !groupedContract.HasOnePass || groupedContract.HasSolver {
		t.Fatalf("one-pass contract selected the wrong phase B backend: %+v", groupedContract)
	}
	if ContractHeapBytes(&groupedContract) != 37757 ||
		ContractStackBytes(&groupedContract) != 6144 ||
		ContractSteps(&groupedContract) != 937980 {
		t.Fatalf("one-pass contract has unexpected totals: %+v", groupedContract)
	}

	ambiguous, err := Compile("(a|ab)(c|bcd)(d*)", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile ambiguous expression failed: %v", err)
	}
	ambiguousContract := ContractFor(&ambiguous, 64)
	if ambiguousContract.HasOnePass || !ambiguousContract.HasSolver {
		t.Fatalf("ambiguous contract selected the wrong phase B backend: %+v", ambiguousContract)
	}
}

func measureHeap(f func()) int64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return int64(after.TotalAlloc - before.TotalAlloc)
}

func TestContractCoversOnePassHeap(t *testing.T) {
	manyGroups := strings.Repeat("(^)", 2047) + "(a+)"
	cases := []struct {
		name    string
		pattern string
		subject string
	}{
		{"small-size-class", "(abc+)", "ab" + strings.Repeat("c", 901)},
		{"window-page-edge", "(abc+)", "ab" + strings.Repeat("c", 4094)},
		{"window-class-edge", "(abc+)", "ab" + strings.Repeat("c", 4095)},
		{"two-window-pages", "(abc+)", "ab" + strings.Repeat("c", 8191)},
		{"three-page-rounding", manyGroups, strings.Repeat("a", 8193)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			re, err := Compile(tc.pattern, LocalePOSIX(), 0)
			if err.Code != ErrNone {
				t.Fatalf("Compile failed: %v", err)
			}
			contract := ContractFor(&re, len(tc.subject))
			if !contract.HasOnePass || contract.HasSolver {
				t.Fatalf("contract selected the wrong phase B backend: %+v", contract)
			}
			measured := measureHeap(func() {
				d := decodeWindow(tc.subject, 0, len(tc.subject))
				caps := make([]Match, re.nsub+1)
				if err := solveCaptures(&re, &d, 0, len(d.runes), 0, caps); err.Code != ErrNone {
					t.Errorf("solveCaptures failed: %v", err)
				}
			})
			if measured > contract.OnePass.HeapBytes {
				t.Errorf("measured %d bytes, one-pass contract %d", measured, contract.OnePass.HeapBytes)
			}
		})
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

// The bound exists exactly for start-anchored programs without newline mode and without a cycle.
func TestProgramDepth(t *testing.T) {
	cases := []struct {
		pattern string
		flags   uint32
		want    int
	}{
		{"^abc$", 0, 3},
		{"^abc$", FlagICase, 3},
		{"^abc$", FlagNoSub, 3},
		{"^abc$", FlagNewline, -1},
		{"^$", 0, 0},
		{"$", 0, 0},
		{"^", 0, 0},
		{"^(a|bc|def)$", 0, 3},
		{"(^a)?$", 0, 1},
		{"^a{3}", 0, 3},
		{"^[ab]{2,4}c", 0, 5},
		{"^.", 0, 1},
		{"^(ab|c)?(de)?$", 0, 4},
		{"abc", 0, -1},
		{"^a|b", 0, -1},
		{"^a*$", 0, -1},
		{"^[a-z]+$", 0, -1},
		{"^(a|b)*b", 0, -1},
		{"^(^)*abc$", 0, 3},
		{"a$", 0, -1},
		{"a?", 0, -1},
		{"^(a?)$", 0, 1},
	}
	for _, tc := range cases {
		re, err := Compile(tc.pattern, LocalePOSIX(), tc.flags)
		if err.Code != ErrNone {
			t.Fatalf("Compile(%q, %d) failed: %v", tc.pattern, tc.flags, err)
		}
		if got := re.prog.depth; got != tc.want {
			t.Errorf("depth of %q with flags %d is %d, want %d", tc.pattern, tc.flags, got, tc.want)
		}
	}
}

// bellmanFordDepth recomputes the longest consuming path by relaxation to a fixed point, and says -1 on a
// consuming cycle.
// It is the certificate check the Lean link performs, with no bound on the rounds, so it does not depend on
// the forward layout that lets progDepth stop after two.
func bellmanFordDepth(pr *program) int {
	n := len(pr.ins)
	depth := make([]int, n)
	for round := 0; round <= n; round++ {
		changed := false
		for pc := range n {
			weight := 0
			if consumingOp(pr.ins[pc].op) {
				weight = 1
			}
			for e := range 2 {
				target, has := progEdge(pr, uint32(pc), e)
				if has && depth[target] < depth[pc]+weight {
					depth[target] = depth[pc] + weight
					changed = true
				}
			}
		}
		if !changed {
			best := 0
			for pc := range n {
				best = max(best, depth[pc])
			}
			return best
		}
	}
	return -1
}

// progDepth agrees with the unbounded relaxation on every program, so the two rounds it stops after are enough.
func TestProgDepthAgreesWithRelaxation(t *testing.T) {
	atoms := []string{"a", "b", ".", "[ab]", "(a|b)", "(ab)?", "a{2}", "(a|bc){1,2}", "^", "$", "a?", "a*", "b+"}
	var patterns []string
	for _, x := range atoms {
		patterns = append(patterns, "^"+x+"$", "^"+x, x+"$", x)
		for _, y := range atoms {
			patterns = append(patterns, "^"+x+y+"$", "^"+x+"|"+y+"$", "^("+x+"|"+y+")?")
		}
	}
	bounded := 0
	for _, pattern := range patterns {
		for _, flags := range []uint32{0, FlagICase, FlagNewline, FlagNoSub} {
			re, err := Compile(pattern, LocalePOSIX(), flags)
			if err.Code != ErrNone {
				continue
			}
			got := progDepth(&re.prog)
			if want := bellmanFordDepth(&re.prog); got != want {
				t.Errorf("progDepth(%q, %d) = %d, relaxation gives %d", pattern, flags, got, want)
			}
			if got >= 0 {
				bounded++
			}
		}
	}
	if bounded < 100 {
		t.Fatalf("only %d bounded programs, the sweep is too small", bounded)
	}
}

// A start-anchored pattern of bounded length pays for depth+3 boundaries instead of one per byte.
func TestAnchoredContractSteps(t *testing.T) {
	re, err := Compile("^abc$", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile failed: %v", err)
	}
	perBoundary := int64(1220)
	for _, tc := range []struct {
		maxInput   int
		boundaries int64
	}{{0, 1}, {1, 2}, {4, 5}, {5, 6}, {6, 6}, {1000, 6}} {
		c := ContractFor(&re, tc.maxInput)
		want := 26 + int64(tc.maxInput) + tc.boundaries*perBoundary
		if c.Matcher.Steps != want {
			t.Errorf("ContractFor(^abc$, %d).Matcher.Steps = %d, want %d", tc.maxInput, c.Matcher.Steps, want)
		}
	}
	unanchored, err := Compile("abc", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile failed: %v", err)
	}
	if got := ContractFor(&unanchored, 1000).Matcher.Steps; got != 26+1000+1001*660 {
		t.Errorf("ContractFor(abc, 1000).Matcher.Steps = %d, want one boundary per byte", got)
	}
}
