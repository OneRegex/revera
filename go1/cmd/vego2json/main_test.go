package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// check runs the translator on one source snippet and returns the
// JSON blob and the violations.
func check(t *testing.T, src string) ([]byte, []string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	blob, violations, err := translate(dir)
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
