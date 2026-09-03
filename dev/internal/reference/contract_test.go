package reference

import (
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

func compileContract(t *testing.T, pattern string, flags CompileFlags, maxInput int) (*Regexp, *Contract) {
	t.Helper()
	re, c, err := CompileWithContract(pattern, locale.POSIX(), flags, maxInput)
	if err != nil {
		t.Fatalf("CompileWithContract(%q) failed: %v", pattern, err)
	}
	if c == nil {
		t.Fatalf("CompileWithContract(%q) returned no contract", pattern)
	}
	return re, c
}

func TestContractBackendSelection(t *testing.T) {
	re, c := compileContract(t, "a*", 0, 100)
	if re.nsub != 0 {
		t.Fatal("pattern must not have subexpressions")
	}
	if c.OnePass != nil || c.Solver != nil {
		t.Error("a zero-group pattern must leave only the matcher backend")
	}

	_, c = compileContract(t, "(a|b)*abb", NoSub, 100)
	if c.OnePass != nil || c.Solver != nil {
		t.Error("NoSub must leave only the matcher backend")
	}

	re, c = compileContract(t, "(a|b)*abb", 0, 100)
	if !re.onePass {
		t.Fatal("pattern must be one-pass")
	}
	if c.OnePass == nil || c.Solver != nil {
		t.Error("a one-pass pattern must report only the walk backend")
	}

	re, c = compileContract(t, "(a|ab)(c|bcd)(d*)", 0, 100)
	if re.onePass {
		t.Fatal("pattern must not be one-pass")
	}
	if c.OnePass != nil {
		t.Error("an ambiguous pattern must not report a walk backend")
	}
	if c.Solver == nil {
		t.Error("an ambiguous pattern must report the solver backend")
	}

	if _, _, err := CompileWithContract("(", locale.POSIX(), 0, 100); err == nil {
		t.Error("a bad pattern must fail")
	}
}

func TestContractOversizedProgram(t *testing.T) {
	pattern := strings.Repeat("a", 1<<20+2)
	re, c := compileContract(t, pattern, 0, 1000)
	if re.prog != nil {
		t.Fatal("the pattern must pass the program size cap")
	}
	if c.Solver != nil || c.OnePass != nil {
		t.Error("without a program, Exec never reaches phase B")
	}
	if c.Matcher.HeapBytes != errorBytes {
		t.Errorf("fallback heap = %d, want %d", c.Matcher.HeapBytes,
			int64(errorBytes))
	}
	if c.Matcher.Steps != 2002 {
		t.Errorf("fallback steps = %d, want 2002", c.Matcher.Steps)
	}
}

func TestContractMonotone(t *testing.T) {
	patterns := []string{
		"abc",
		"(a|b)*abb",
		"(a|ab)(c|bcd)(d*)",
		"([ab]*)-([ab]{4})",
		"((a?){5,250}){10}",
	}
	sizes := []int{0, 1, 64, 4096, 1 << 20, int(subjectLimit)}
	for _, pattern := range patterns {
		re, _ := compileContract(t, pattern, 0, 0)
		var prev *Contract
		for _, size := range sizes {
			c := re.newContract(size)
			if c.HeapBytes() <= 0 || c.StackBytes() <= 0 || c.Steps() <= 0 {
				t.Fatalf("%q maxInput=%d: nonpositive figure", pattern, size)
			}
			if prev != nil {
				if c.HeapBytes() < prev.HeapBytes() ||
					c.StackBytes() < prev.StackBytes() ||
					c.Steps() < prev.Steps() {
					t.Errorf("%q: figures shrank when maxInput grew to %d",
						pattern, size)
				}
			}
			prev = c
		}
	}
}

func TestContractClampAndSaturation(t *testing.T) {
	_, atLimit := compileContract(t, "(a|ab)*", 0, int(subjectLimit))
	_, beyond := compileContract(t, "(a|ab)*", 0, math.MaxInt)
	if beyond.Matcher != atLimit.Matcher || *beyond.Solver != *atLimit.Solver ||
		beyond.MaxInput != int(subjectLimit) {
		t.Error("maxInput beyond the subject limit must clamp")
	}

	_, negative := compileContract(t, "(a|ab)*", 0, -5)
	_, zero := compileContract(t, "(a|ab)*", 0, 0)
	if negative.Matcher != zero.Matcher || negative.MaxInput != 0 {
		t.Error("a negative maxInput must clamp to zero")
	}

	if c := cAdd(contractCap, contractCap); c != contractCap {
		t.Errorf("cAdd must saturate, got %d", c)
	}
	if c := cMul(contractCap, contractCap); c != contractCap {
		t.Errorf("cMul must saturate, got %d", c)
	}
	if c := cMul(contractCap, 0); c != 0 {
		t.Errorf("cMul(cap, 0) = %d, want 0", c)
	}
}

// The fixed byte constants describe 64-bit layouts of types that live in other files.
// unsafe stays out of the shipped figures, so those figures work on every platform.
// Here it anchors them, so a grown type fails a test instead of quietly shrinking a contract.
func TestContractSizeConstants(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("the layout constants describe 64-bit platforms")
	}
	if s := int64(unsafe.Sizeof(ptree{})); s != ptreeBytes {
		t.Errorf("ptreeBytes = %d, ptree needs %d", int64(ptreeBytes), s)
	}
	if s := int64(unsafe.Sizeof(Match{})); s != matchBytes {
		t.Errorf("matchBytes = %d, Match needs %d", int64(matchBytes), s)
	}
	if s := int64(unsafe.Sizeof(Error{})); s > errorBytes {
		t.Errorf("errorBytes = %d, Error needs %d", int64(errorBytes), s)
	}
	const sliceHeader = 24
	memoEntries := []int64{
		int64(unsafe.Sizeof(memoKey{})) + 8,
		int64(unsafe.Sizeof(concatKey{})) + sliceHeader,
		int64(unsafe.Sizeof(repKey{})) + int64(unsafe.Sizeof(repResult{})),
	}
	for i, s := range memoEntries {
		if s > mapEntryBytes {
			t.Errorf("mapEntryBytes = %d, memo record %d needs %d",
				int64(mapEntryBytes), i, s)
		}
	}
	if s := int64(unsafe.Sizeof(slotTable{})); s > 112 {
		t.Errorf("workspaceHeapBound counts 112 per slot, slotTable needs %d", s)
	}
	if s := int64(unsafe.Sizeof(engineWS{})); s > 256 {
		t.Errorf("workspaceHeapBound counts 256 for the workspace, engineWS needs %d", s)
	}
	if s := int64(unsafe.Sizeof(decoded{})); s > 64 {
		t.Errorf("captureHeap counts 64 for the window struct, decoded needs %d", s)
	}
}

// measureHeap returns the bytes one run of f allocates.
func measureHeap(f func()) int64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return int64(after.TotalAlloc - before.TotalAlloc)
}

// The matcher contract must cover the measured workspace.
// That includes the allocator and header overhead the estimate leaves out.
// A pattern of this size gives the payload enough weight over the fixed overhead.
func TestContractCoversMatcherHeap(t *testing.T) {
	pattern := strings.Repeat("a", 200)
	subject := strings.Repeat("b", 1000)
	re, c := compileContract(t, pattern, 0, len(subject))
	measured := measureHeap(func() {
		if _, err := re.Exec(subject, nil, 0); err != nil {
			t.Error(err)
		}
	})
	if measured > c.Matcher.HeapBytes {
		t.Errorf("measured %d bytes, contract %d", measured, c.Matcher.HeapBytes)
	}
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
			re, c := compileContract(t, tc.pattern, 0, len(tc.subject))
			if !re.onePass || c.OnePass == nil || c.Solver != nil {
				t.Fatalf("contract selected the wrong phase B backend: %+v", c)
			}
			measured := measureHeap(func() {
				d := decodeSubject(tc.subject)
				caps := make([]Match, re.nsub+1)
				if err := re.solveCaptures(&d, 0, len(d.runes), 0, caps); err != nil {
					t.Errorf("solveCaptures failed: %v", err)
				}
			})
			if measured > c.OnePass.HeapBytes {
				t.Errorf("measured %d bytes, one-pass contract %d", measured, c.OnePass.HeapBytes)
			}
		})
	}
}

func TestContractCoversSolverHeap(t *testing.T) {
	subject := strings.Repeat("ab", 400) + "-abab"
	re, _ := compileContract(t, "([ab]*)-([ab]{4})", 0, len(subject))
	// Force the solver, like the phase B benchmarks do.
	re.onePass = false
	c := re.newContract(len(subject))
	pmatch := make([]Match, 3)
	measured := measureHeap(func() {
		matched, err := re.Exec(subject, pmatch, 0)
		if err != nil || !matched {
			t.Errorf("Exec = %v, %v", matched, err)
		}
	})
	if measured > c.HeapBytes() {
		t.Errorf("measured %d bytes, contract %d", measured, c.HeapBytes())
	}
}

func TestContractCoversOnePassWork(t *testing.T) {
	cases := []struct {
		pattern string
		subject string
		loops   int64
	}{
		{"(a)(b)(c)", "abc", 6},
		{"((ab){1,3})c", "ababc", -1},
		{"((a)|b)*", "ab", -1},
		{"((a)|(b))c", "bc", -1},
	}
	for _, tc := range cases {
		re, c := compileContract(t, tc.pattern, 0, len(tc.subject))
		if !re.onePass || c.OnePass == nil || c.Solver != nil {
			t.Fatalf("%q must select only the one-pass backend", tc.pattern)
		}
		d := decodeSubject(tc.subject)
		caps := make([]Match, re.nsub+1)
		var measured onePassWork
		if !re.onePassCapsMeasured(&d, re.root, 0, len(d.runes), 0, caps, &measured) {
			t.Fatalf("onePassCapsMeasured(%q, %q) failed", tc.pattern, tc.subject)
		}
		work := measured.calls + measured.loops
		if work > c.OnePass.Steps {
			t.Errorf("%q on %q: work %d passed the step bound %d",
				tc.pattern, tc.subject, work, c.OnePass.Steps)
		}
		if tc.loops >= 0 && measured.loops != tc.loops {
			t.Errorf("%q on %q: loops %d, want %d",
				tc.pattern, tc.subject, measured.loops, tc.loops)
		}
	}
}

// The structural step bound must cover the work counter of a real solver run.
// Exec cannot show that counter, so the test drives the solver the way solveCaptures does.
// It runs a full-window parse with a pooled solver, and returns the solver to the pool afterwards.
func TestContractCoversSolverWork(t *testing.T) {
	cases := []struct {
		pattern  string
		flags    CompileFlags
		subjects []string
	}{
		{"(a*|ab)*(c|bcd)(d*)", 0,
			[]string{"abcd", "abbcdddd", strings.Repeat("a", 40) + "bcd"}},
		{"((a|é)*)(é|b)*", ICase, []string{"aéÉaébb"}},
		{"^((a?)*b|ab*)+$", 0, []string{"aabab"}},
	}
	for _, tc := range cases {
		re, c, err := CompileWithContract(tc.pattern, locale.POSIX(),
			tc.flags, 64)
		if err != nil {
			t.Fatalf("CompileWithContract(%q) failed: %v", tc.pattern, err)
		}
		if re.onePass {
			t.Fatalf("%q must use the solver", tc.pattern)
		}
		for _, subject := range tc.subjects {
			d := decodeSubject(subject)
			s := re.getSolver()
			s.re, s.d, s.eflags = re, &d, 0
			s.bestParse(re.root, 0, len(d.runes))
			work := int64(s.work)
			s.d = nil
			re.capPool.Put(s)
			if work > c.Solver.Steps {
				t.Errorf("%q on %q: work %d passed the step bound %d",
					tc.pattern, subject, work, c.Solver.Steps)
			}
			bound := solverSteps(re.root, int64(len(subject)))
			if work > bound {
				t.Errorf("%q on %q: work %d passed the structural bound %d",
					tc.pattern, subject, work, bound)
			}
		}
	}
}

// Compaction must keep one entry per instruction, keep the queued set, and leave the scratch marks zero.
// These properties carry the relaxation fixpoint.
// An improved instruction that nothing processed yet stays queued at least once.
func TestCompactQueue(t *testing.T) {
	e := &phaseA{ws: &engineWS{
		onq:   make([]bool, 8),
		queue: []uint32{3, 1, 3, 2, 1, 1, 5, 3},
	}}
	e.compactQueue()
	if want := []uint32{3, 1, 2, 5}; !slices.Equal(e.ws.queue, want) {
		t.Fatalf("queue = %v, want %v", e.ws.queue, want)
	}
	for pc, mark := range e.ws.onq {
		if mark {
			t.Errorf("onq[%d] still set after compaction", pc)
		}
	}
}

// The closure queue must stay linear in the program.
// The matcher heap contract depends on that bound.
func TestContractQueueStaysLinear(t *testing.T) {
	subject := strings.Repeat("ab", 2048) + "abb"
	re, err := Compile("(a|b)*(ab|ba)*abb", locale.POSIX(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// This workspace is owned, because the pooled one can vanish under GC pressure.
	ws := &engineWS{}
	if !re.runPhaseAWith(ws, subject, 0).matched {
		t.Fatal("phase A must match")
	}
	limit := 2*queueCompactFactor*len(re.prog.ins) + 4
	if cap(ws.queue) > limit {
		t.Errorf("queue capacity %d passed the linear bound %d",
			cap(ws.queue), limit)
	}
	for _, mark := range ws.onq {
		if mark {
			t.Fatal("compaction scratch must stay clear between calls")
		}
	}
}

// The POSIX bracket figures are fixed.
// A full locale's depend on its tables, so only their order is checked.
func TestBracketAtomCost(t *testing.T) {
	czech, ok := locale.Open("cs", "")
	if !ok {
		t.Fatal("locale.Open(cs) failed")
	}
	atom := func(pattern string, loc locale.Locale, flags CompileFlags) int64 {
		re, err := Compile(pattern, loc, flags)
		if err != nil {
			t.Fatalf("Compile(%q) failed: %v", pattern, err)
		}
		return atomCost(re.root)
	}
	if got := atom("[a-z]", locale.POSIX(), 0); got != 4 {
		t.Errorf("atomCost([a-z]) = %d, want 4", got)
	}
	if got := atom("[a-z]", locale.POSIX(), ICase); got != 10 {
		t.Errorf("atomCost([a-z], ICase) = %d, want 10", got)
	}
	plain := atom("[[=a=]]", czech, 0)
	if plain <= atom("[[=a=]]", locale.POSIX(), 0) || plain < 1000 {
		t.Errorf("atomCost([[=a=]], cs) = %d, expected the locale searches to show", plain)
	}
	if folded := atom("[[=a=]]", czech, ICase); folded <= plain {
		t.Errorf("atomCost([[=a=]], cs, ICase) = %d, not above %d", folded, plain)
	}
	two := atom("[[=a=][=b=]]", czech, 0)
	three := atom("[[=a=][=b=][=c=]]", czech, 0)
	if two-plain != three-two {
		t.Errorf("equivalence classes cost %d then %d", two-plain, three-two)
	}
}
