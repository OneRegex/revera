package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
)

// check runs the translator on one source snippet.
// It returns the JSON blob and the violations.
func check(t *testing.T, src string) ([]byte, []string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	blob, violations, err := Package(dir)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if violations == nil && blob == nil {
		t.Fatal("no output and no violations")
	}
	return blob, violations
}

func TestAcceptsConformingCode(t *testing.T) {
	src := `package p

const limit int = 10

var names = [2]string{"a", "b"}

type Pair struct {
	A int32
	B []int32
}

func sum(p *Pair, extra []int32) (int, bool) {
	total := 0
	for i := 0; i < len(p.B); i++ {
		total += int(p.B[i])
	}
	for _, v := range extra {
		total += int(v)
	}
	switch total {
	case 0:
		return 0, false
	default:
	}
	p.B = append(p.B, int32(total))
	p.B = p.B[:len(p.B)-1]
	buf := make([]int32, 0, 4)
	buf = append(buf, extra...)
	return total + limit + len(names[0]), true
}
`
	blob, violations := check(t, src)
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %v", violations)
	}
	var doc map[string]any
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if doc["package"] != "p" || doc["vego"] != float64(1) {
		t.Fatalf("bad JSON header: %v", doc)
	}
	if len(doc["funcs"].([]any)) != 1 {
		t.Fatal("expected one function in the JSON")
	}
}

func TestAcceptsKeyedConstantComposite(t *testing.T) {
	src := `package p

type item struct {
	Value int
}

var items = [1]item{item{Value: 7}}

func first() int {
	for range 1 {
	}
	return items[0].Value
}
`
	if _, violations := check(t, src); len(violations) != 0 {
		t.Fatalf("expected no violations, got %v", violations)
	}
}

func TestAcceptsRepresentableIntegerDefaults(t *testing.T) {
	src := `package p

const maxInt = 1<<63 - 1
const highBit uint64 = 1 << 63

func values() (int, uint64) {
	return maxInt, highBit
}
`
	if _, violations := check(t, src); len(violations) != 0 {
		t.Fatalf("expected no violations, got %v", violations)
	}
}

func TestRejections(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"import", "package p\n\nimport \"fmt\"\n\nfunc f() { fmt.Println() }\n",
			"imports"},
		{"method", "package p\n\ntype T struct{ X int }\n\nfunc (t *T) f() int { return t.X }\n",
			"methods"},
		{"map", "package p\n\nfunc f() int {\n\tm := map[int]int{}\n\treturn m[0]\n}\n",
			"outside the subset"},
		{"closure", "package p\n\nfunc f() int {\n\tg := func() int { return 1 }\n\treturn g()\n}\n",
			"outside the subset"},
		{"iota", "package p\n\nconst (\n\ta int = iota\n)\n\nfunc f() int { return a }\n",
			"iota"},
		{"named scalar type", "package p\n\ntype code int32\n\nfunc f() code { return 0 }\n",
			"struct type declarations"},
		{"pointer field", "package p\n\ntype S struct{ X int }\n\ntype T struct{ P *S }\n\nfunc f(t *T) bool { return t.P == nil }\n",
			"pointer fields"},
		{"three results", "package p\n\nfunc f() (int, int, int) { return 1, 2, 3 }\n",
			"more than two results"},
		{"defer", "package p\n\nfunc g() {}\n\nfunc f() { defer g() }\n",
			"statement outside the subset"},
		{"goto", "package p\n\nfunc f() {\nagain:\n\tgoto again\n}\n",
			"statement outside the subset"},
		{"global write", "package p\n\nvar g = 1\n\nfunc f() { g = 2 }\n",
			"immutable"},
		{"switch break", "package p\n\nfunc f(x int) int {\n\tswitch x {\n\tcase 1:\n\t\tbreak\n\t}\n\treturn x\n}\n",
			"break must target a loop"},
		{"float", "package p\n\nfunc f() float64 { return 1.5 }\n",
			"not in the subset"},
		{"rune type", "package p\n\nfunc f() rune { return 'a' }\n",
			"not in the subset"},
		{"variadic", "package p\n\nfunc f(xs ...int) int { return len(xs) }\n",
			"variadic"},
		{"three-index slice", "package p\n\nfunc f(s []int) []int { return s[0:1:1] }\n",
			"three-index"},
		{"if init", "package p\n\nfunc g() int { return 1 }\n\nfunc f() int {\n\tif x := g(); x > 0 {\n\t\treturn x\n\t}\n\treturn 0\n}\n",
			"init statement"},
		{"view stored in field", "package p\n\ntype T struct{ B []int32 }\n\nfunc f(t *T, s []int32) {\n\tt.B = s[1:2]\n}\n",
			"fresh buffer"},
		{"address of global", "package p\n\ntype S struct{ X int }\n\nvar g = S{X: 1}\n\nfunc h(p *S) int { return p.X }\n\nfunc f() int { return h(&g) }\n",
			"package-level"},
		{"unkeyed struct literal", "package p\n\ntype S struct{ X int }\n\nfunc f() int {\n\ts := S{1}\n\treturn s.X\n}\n",
			"field keys"},
		{"string range", "package p\n\nfunc f(s string) int {\n\tn := 0\n\tfor range s {\n\t\tn++\n\t}\n\treturn n\n}\n",
			"range is only over"},
		{"non-int count range", "package p\n\nfunc f(n int64) {\n\tfor range n {\n\t}\n}\n",
			"range is only over"},
		{"pointer comparison", "package p\n\ntype S struct{ X int }\n\nfunc f(a, b *S) bool { return a == b }\n",
			"pointer comparisons"},
		{"string addition", "package p\n\nfunc f(a, b string) string { return a + b }\n",
			"string operators"},
		{"string switch", "package p\n\nfunc f(s string) int {\n\tswitch s {\n\tcase \"x\":\n\t\treturn 1\n\t}\n\treturn 0\n}\n",
			"switch tag"},
		{"bodyless declaration", "package p\n\nfunc f()\n",
			"without a body"},
		{"oversized untyped integer", "package p\n\nconst huge = 1 << 63\n",
			"does not fit the Vego int default"},
		{"tuple namespace", "package p\n\ntype Tup_i64_i64 struct{ Value int }\n",
			"reserved for generated code"},
		{"runtime name", "package p\n\nfunc mem() {}\n",
			"reserved for generated code"},
		{"labels", "package p\n\nfunc f() {\nouter:\n\tfor {\n\t\tbreak outer\n\t}\n}\n",
			"outside the subset"},
		{"pointer result", "package p\n\ntype S struct{ X int }\n\nfunc f(s S) *S { return &s }\n",
			"parameters"},
		{"pointer local", "package p\n\ntype S struct{ X int }\n\nfunc g(p *S) {}\n\nfunc f() {\n\tvar s S\n\tp := &s\n\tg(p)\n}\n",
			"call argument"},
		{"global slice", "package p\n\nvar g = []int{1}\n\nfunc f() int { return g[0] }\n",
			"must not contain slices"},
		{"global passed as slice", "package p\n\nvar g = [1]int{1}\n\nfunc mutate(v []int) { v[0] = 2 }\n\nfunc f() { mutate(g[:]) }\n",
			"package-level data"},
		{"rune-slice conversion", "package p\n\nfunc f(a []int32) string { return string(a) }\n",
			"conversion outside the subset"},
		{"assign range", "package p\n\nfunc f() {\n\ta := []int{1}\n\tfor a[0], _ = range []int{1, 2} {\n\t}\n}\n",
			"range must"},
		{"empty append", "package p\n\nfunc f() {\n\tvar a []int\n\ta = append(a)\n\t_ = a\n}\n",
			"append needs at least one element"},
		{"invalid UTF-8 literal", "package p\n\nfunc f() string { return \"\\xff\" }\n",
			"not valid UTF-8"},
		{"shadowed true", "package p\n\nfunc f() bool {\n\ttrue := 1 < 0\n\treturn true\n}\n",
			"predeclared"},
		{"scalar-named struct", "package p\n\ntype int32 struct{ X int }\n",
			"predeclared"},
		{"non-ASCII identifier", "package p\n\nfunc f() int {\n\tcaf\u00e9 := 1\n\treturn caf\u00e9\n}\n",
			"not ASCII"},
		{"pointer copy", "package p\n\ntype S struct{ X int }\n\nfunc f(p *S) int {\n\tq := p\n\treturn q.X\n}\n",
			"pointer locals"},
		{"address in a slice element", "package p\n\ntype In struct{ V int }\n\ntype Row struct{ In In }\n\nfunc g(p *In) {}\n\nfunc f(rows []Row) {\n\tg(&rows[0].In)\n}\n",
			"slice element"},
		{"string compound assignment", "package p\n\nfunc f(s string) string {\n\ts += \"x\"\n\treturn s\n}\n",
			"compound assignment"},
		{"cap of an array", "package p\n\nfunc f() int {\n\tvar a [4]int\n\treturn cap(a)\n}\n",
			"cap applies only to slices"},
		{"min of strings", "package p\n\nfunc f(a, b string) string { return min(a, b) }\n",
			"applies only to integers"},
		{"view of a loop variable", "package p\n\nfunc f() int {\n\tvar keep []int\n\tfor a := [2]int{}; a[0] < 3; a[0]++ {\n\t\tif a[0] == 0 {\n\t\t\tkeep = a[:]\n\t\t}\n\t}\n\treturn keep[0]\n}\n",
			"outlive the array"},
		{"view of a range value", "package p\n\nfunc f(rows [3][2]int) int {\n\tvar first []int\n\tfor i, v := range rows {\n\t\tif i == 0 {\n\t\t\tfirst = v[:]\n\t\t}\n\t}\n\treturn first[0]\n}\n",
			"outlive the array"},
		{"view of a block-local array", "package p\n\nfunc f() int {\n\tvar s []int\n\tfor i := 0; i < 2; i++ {\n\t\tvar a [2]int\n\t\ta[0] = i\n\t\tif i == 0 {\n\t\t\ts = a[:]\n\t\t}\n\t}\n\treturn s[0]\n}\n",
			"outlive the array"},
		{"view passed through a local", "package p\n\nfunc f() int {\n\tvar s []int\n\t{\n\t\tvar a [2]int\n\t\tv := a[:]\n\t\ts = append(v[:0], 1)\n\t}\n\treturn s[0]\n}\n",
			"outlive the array"},
		{"returned view of a local array", "package p\n\nfunc g() []int {\n\tvar a [4]int\n\treturn a[:]\n}\n",
			"must not be returned"},
		{"view returned through append", "package p\n\nfunc g() []int {\n\tvar a [4]int\n\tv := a[:0]\n\treturn append(v, 1)\n}\n",
			"must not be returned"},
		{"view in a composite literal", "package p\n\ntype S struct{ s []int }\n\nfunc f() S {\n\tvar a [4]int\n\treturn S{s: a[:]}\n}\n",
			"stored in a field or an element"},
		{"view moved into a field", "package p\n\ntype S struct{ s []int }\n\nfunc f() int {\n\tvar o S\n\tvar a [4]int\n\tv := a[:]\n\to.s = v\n\treturn o.s[0]\n}\n",
			"stored in a field or an element"},
		{"view stored behind a pointer", "package p\n\ntype S struct{ s []int }\n\nfunc put(p *S) {\n\tvar a [4]int\n\tv := a[:]\n\tp.s = v\n}\n",
			"stored in a field or an element"},
		{"parameter stored into another parameter", "package p\n\ntype S struct{ s []int }\n\nfunc keep(p *S, v []int) { p.s = v }\n",
			"stored in a field or an element"},
		{"view handed back by a call", "package p\n\nfunc same(b []int) []int { return b }\n\nfunc f() int {\n\tvar o []int\n\tfor i := 0; i < 2; i++ {\n\t\tvar a [4]int\n\t\to = same(a[:])\n\t}\n\treturn o[0]\n}\n",
			"outlive the array"},
		{"struct holding a view passed by value", "package p\n\ntype S struct{ s []int }\n\nfunc use(x S) int { return len(x.s) }\n\nfunc mk(v []int) S { return S{s: v} }\n\nfunc f() int {\n\tvar a [4]int\n\tit := mk(a[:])\n\treturn use(it)\n}\n",
			"stored in a field or an element"},
		{"range over an indexed array", "package p\n\nfunc f(a [][3]int) int {\n\tn := 0\n\tfor i := range a[5] {\n\t\tn += i\n\t}\n\treturn n\n}\n",
			"range over an indexed array"},
		{"short declaration reuse", "package p\n\nfunc g() (int, int) { return 1, 2 }\n\nfunc f() int {\n\ta := 0\n\ta, b := g()\n\treturn a + b\n}\n",
			"reuses a"},
	}
	for _, tc := range cases {
		_, violations := check(t, tc.src)
		if len(violations) == 0 {
			t.Errorf("%s: expected a violation, got none", tc.name)
			continue
		}
		found := false
		for _, v := range violations {
			if strings.Contains(v, tc.want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: no violation mentions %q; got %v", tc.name, tc.want, violations)
		}
	}
}

// checkAll runs the translator and then the checks of the emitters, which vegoc check also runs.
func checkAll(t *testing.T, src string) error {
	t.Helper()
	blob, violations := check(t, src)
	if len(violations) > 0 {
		return fmt.Errorf("%v", violations)
	}
	p, err := compiler.Load(blob)
	if err != nil {
		return err
	}
	return compiler.Check(p)
}

func TestNamesThatStayAvailable(t *testing.T) {
	// Fields may reuse predeclared names, and locals may hide builtin functions, which the JSON form never calls by a local name.
	src := "package p\n\ntype T struct{ min, max, nil int }\n\nfunc f(t T) int {\n\tany := t.min\n\tprint := t.max + t.nil\n\treturn any + print\n}\n"
	if err := checkAll(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestTwoValueAssignOrder(t *testing.T) {
	const prelude = "package p\n\ntype Box struct {\n\tn, m int\n\tarr [4]int\n\ts []int\n}\n\nfunc pair() (int, int) { return 2, 7 }\n\n"
	cases := []struct {
		body   string
		reject bool
	}{
		{"b.n, b.arr[b.n] = pair()", true},
		{"b.arr[0], b.arr[b.arr[0]] = pair()", true},
		{"b.s, b.s[0] = pair2()", true},
		{"b.n, b.m = pair()", false},
		{"b.n, b.arr[b.m] = pair()", false},
		{"b.arr[0], b.arr[1] = pair()", false},
		{"b.s[0], b.s[1] = pair()", false},
	}
	for _, tc := range cases {
		src := prelude + "func pair2() ([]int, int) { return nil, 1 }\n\nfunc f(b *Box) {\n\t" + tc.body + "\n}\n"
		err := checkAll(t, src)
		if tc.reject && (err == nil || !strings.Contains(err.Error(), "cannot order")) {
			t.Errorf("%s: expected the order violation, got %v", tc.body, err)
		}
		if !tc.reject && err != nil {
			t.Errorf("%s: unexpected error %v", tc.body, err)
		}
	}
}

func TestEvalOrder(t *testing.T) {
	const prelude = "package p\n\ntype S struct {\n\ti   int\n\tarr [4]int\n\tbuf []int\n}\n\n" +
		"func bump(p *S) int {\n\tp.i++\n\treturn p.i\n}\n\n" +
		"func peek(p *S) int { return p.i }\n\n" +
		"func fill(b []int) int {\n\tb[0] = 9\n\treturn 1\n}\n\n" +
		"func two(a, b int) int { return a + b }\n\n"
	cases := []struct {
		body   string
		reject bool
	}{
		{"v := s.i + bump(&s)\n\t_ = v", true},
		{"s.i += bump(&s)", true},
		{"s.arr[s.i] = bump(&s)", true},
		{"v := two(s.i, bump(&s))\n\t_ = v", true},
		{"v := s.buf[0] + fill(s.buf)\n\t_ = v", true},
		{"v := bump(&s) + s.i\n\t_ = v", false},
		{"ok := s.i > 0 && bump(&s) > 0\n\t_ = ok", false},
		{"v := s.i + peek(&s)\n\t_ = v", false},
		{"v := two(bump(&s), bump(&s))\n\t_ = v", false},
		{"s.buf = append(s.buf, s.buf[0])", false},
		{"v := s.arr[0] + fill(s.buf)\n\t_ = v", false},
	}
	for _, tc := range cases {
		src := prelude + "func f() {\n\tvar s S\n\ts.buf = make([]int, 1)\n\t" + tc.body + "\n}\n"
		err := checkAll(t, src)
		if tc.reject && (err == nil || !strings.Contains(err.Error(), "order Go leaves unspecified")) {
			t.Errorf("%q: expected the order violation, got %v", tc.body, err)
		}
		if !tc.reject && err != nil {
			t.Errorf("%q: unexpected error %v", tc.body, err)
		}
	}
}

func TestEvalOrderFollowsSourceOrder(t *testing.T) {
	// The slice header is read before the index or the bounds that call bump.
	const prelude = "package p\n\ntype S struct{ buf []int }\n\nfunc bump(p *S) int {\n\tp.buf = append(p.buf, 1)\n\treturn 0\n}\n\n"
	for _, use := range []string{"v := s.buf[bump(&s)]", "v := s.buf[bump(&s):]"} {
		src := prelude + "func f() {\n\tvar s S\n\ts.buf = make([]int, 1)\n\t" + use + "\n\t_ = v\n}\n"
		if err := checkAll(t, src); err == nil || !strings.Contains(err.Error(), "order Go leaves unspecified") {
			t.Errorf("%s: expected the order violation, got %v", use, err)
		}
	}
}

func TestConstantFoldingFollowsGoTypes(t *testing.T) {
	// k is untyped until the declaration converts it, and b and w are typed through their values.
	src := "package p\n\nconst a uint8 = 1\n\nconst b = a\n\nconst w = uint16(3)\n\n" +
		"const k uint32 = (-(^1048576)) >> 8\n\nconst nb = ^b\n\nconst nw = ^w\n"
	blob, violations := check(t, src)
	if len(violations) > 0 {
		t.Fatal(violations)
	}
	p, err := compiler.Load(blob)
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.Check(p); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int64{"k": 4096, "nb": 254, "nw": 65532} {
		if got := p.ConstMap[name].ConstVal; got == nil || got.Int64() != want {
			t.Errorf("%s folds to %v, want %d", name, got, want)
		}
	}
}

func TestViewsThatStayInsideTheirArrayScope(t *testing.T) {
	loop := "package p\n\nfunc sum(v []int) int { return v[0] + v[1] }\n\nfunc f() int {\n\tn := 0\n" +
		"\tfor a := [2]int{1, 2}; a[0] < 3; a[0]++ {\n\t\tn += sum(a[:])\n\t\tw := a[:]\n\t\tn += w[0]\n\t}\n" +
		"\tvar b [2]int\n\tvar s []int\n\ts = b[:]\n\treturn n + s[0]\n}\n"
	// Views passed to calls, move-through, a view returned from a pointer parameter, and self-appends.
	calls := "package p\n\ntype S struct {\n\ts   []int\n\tarr [4]int\n}\n\nfunc sum(v []int) int {\n\tt := 0\n\tfor i := 0; i < len(v); i++ {\n\t\tt += v[i]\n\t}\n\treturn t\n}\n\nfunc push(s []int, x int) []int { return append(s, x) }\n\nfunc head(p *S, n int) []int { return p.arr[:n] }\n\nfunc grow(p *S, x int) { p.s = append(p.s, x) }\n\nfunc F() int {\n\tvar a [4]int\n\ta[0] = 1\n\tv := a[:]\n\tn := sum(v) + sum(a[:2])\n\tvar buf []int\n\tbuf = push(buf, 3)\n\tvar st S\n\tst.arr[1] = 5\n\tn += sum(head(&st, 2))\n\tgrow(&st, 4)\n\tfor i := 0; i < 2; i++ {\n\t\tvar b [2]int\n\t\tb[0] = i\n\t\tn += sum(b[:])\n\t\tu := b[:]\n\t\tn += u[0]\n\t}\n\treturn n + len(st.s)\n}\n"
	for _, src := range []string{loop, calls} {
		if err := checkAll(t, src); err != nil {
			t.Fatal(err)
		}
	}
}

// TestViewLifetimes checks that no view of a local array outlives it, through returns, calls, stores and appends.
// Each rejected program lets a view of a local array, or of what a parameter points to, land somewhere that can outlive it.
func TestViewLifetimes(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		reject bool
	}{
		{"two value return", "package p\n\nfunc g() ([]int, int) {\n\tvar a [4]int\n\treturn a[:], 1\n}\n", true},
		{"two value call", "package p\n\nfunc two(b []int) ([]int, int) { return b, 0 }\n\nfunc F() int {\n\tvar o []int\n\tfor i := 0; i < 2; i++ {\n\t\tvar a [4]int\n\t\to, _ = two(a[:])\n\t}\n\treturn o[0]\n}\n", true},
		{"wrapper self view", "package p\n\ntype S struct{ arr [4]int }\n\nfunc view(p *S) []int { return p.arr[:] }\n\nfunc G(p *S) []int { return view(p) }\n\nfunc F() []int {\n\tvar s S\n\treturn G(&s)\n}\n", true},
		{"append into param buffer", "package p\n\nfunc put(dst [][]int) int {\n\tvar a [4]int\n\tx := append(dst[:0], a[:])\n\treturn len(x)\n}\n", true},
		{"blank append into param", "package p\n\nfunc put(dst [][]int) {\n\tvar a [4]int\n\t_ = append(dst[:0], a[:])\n}\n", true},
		{"self struct view", "package p\n\ntype S struct {\n\tarr [4]int\n\ts   []int\n}\n\nfunc F() S {\n\tvar x S\n\tv := x.arr[:]\n\tx.s = v\n\treturn x\n}\n", true},
		{"pointer self view", "package p\n\ntype S struct {\n\tarr [4]int\n\ts   []int\n}\n\nfunc f(p *S) {\n\tv := p.arr[:]\n\tp.s = v\n}\n", true},
		{"param buffer into field", "package p\n\ntype S struct{ s []int }\n\nfunc f(p *S, s []int) {\n\tt := s[1:]\n\tp.s = t\n}\n", true},
		{"byvalue array param", "package p\n\nfunc g(a [4]int) []int { return a[:] }\n", true},
		{"param into local struct", "package p\n\ntype S struct{ s []int }\n\nfunc mk(v []int) S {\n\tvar x S\n\tx.s = v\n\treturn x\n}\n", true},
		{"move through", "package p\n\nfunc push(s []int, x int) []int { return append(s, x) }\n\nfunc F() int {\n\tvar buf []int\n\tbuf = push(buf, 1)\n\tbuf = push(buf, 2)\n\treturn buf[1]\n}\n", false},
		{"builder self append", "package p\n\ntype B struct{ items [][]int }\n\nfunc add(b *B, n int) {\n\trow := make([]int, n)\n\tb.items = append(b.items, row)\n}\n\nfunc F() int {\n\tvar b B\n\tadd(&b, 3)\n\treturn len(b.items[0])\n}\n", false},
		{"view to callee", "package p\n\nfunc sum(v []int) int {\n\tt := 0\n\tfor i := 0; i < len(v); i++ {\n\t\tt += v[i]\n\t}\n\treturn t\n}\n\nfunc F() int {\n\tvar a [8]int\n\ta[3] = 4\n\tn := 0\n\tfor i := 0; i < 2; i++ {\n\t\tvar b [2]int\n\t\tb[1] = i\n\t\tn += sum(b[:]) + sum(a[2:4])\n\t}\n\treturn n\n}\n", false},
		{"read owner field", "package p\n\ntype S struct{ s []int }\n\ntype B struct{ items []S }\n\nfunc first(b *B) []int { return b.items[0].s }\n\nfunc copyOut(b *B, dst *B) { dst.items = append(dst.items, b.items[0]) }\n\nfunc F() int {\n\tvar b B\n\tb.items = append(b.items, S{s: make([]int, 2)})\n\tvar d B\n\tcopyOut(&b, &d)\n\treturn len(first(&b)) + len(d.items)\n}\n", false},
		{"return owner field", "package p\n\ntype S struct{ s []int }\n\nfunc first(p *S) []int { return p.s }\n\nfunc F() []int {\n\tvar x S\n\tx.s = make([]int, 3)\n\treturn first(&x)\n}\n", false},
		{"int from two value", "package p\n\nfunc two(b []int) ([]int, int) { return b, len(b) }\n\nfunc F() int {\n\tvar a [4]int\n\tv, n := two(a[:])\n\treturn n + v[0]\n}\n", false},
		{"self view through call", "package p\n\ntype S struct {\n\tarr [4]int\n\ts   []int\n}\n\nfunc view(p *S) []int { return p.arr[:] }\n\nfunc f(p *S) { p.s = view(p) }\n", true},
		{"param wrapped in struct", "package p\n\ntype S struct{ s []int }\n\nfunc wrap(v []int) S { return S{s: v} }\n", true},
	}
	for _, tc := range cases {
		err := checkAll(t, tc.src)
		if tc.reject && err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
		if !tc.reject && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
