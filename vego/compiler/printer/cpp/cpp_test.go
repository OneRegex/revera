package cpp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
)

func TestLoopPostAcceptsCallStatement(t *testing.T) {
	g := &gen{p: &compiler.Program{}}
	post := &compiler.Stmt{
		K:     "expr_stmt",
		Value: &compiler.Expr{K: "call", Name: "tick"},
	}
	if got := g.inlineStmt(post); got != "tick()" {
		t.Fatalf("inline loop post = %q, want %q", got, "tick()")
	}
}

func TestSyntheticTempsAvoidProgramNames(t *testing.T) {
	f := &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{"_t1": {}}}
	g := &gen{p: &compiler.Program{Funcs: []*compiler.FuncDecl{f}}}
	g.resetNames(f)
	if got := g.newTmp(); got != "_t2" {
		t.Fatalf("first synthetic temporary = %q, want %q", got, "_t2")
	}
}

func TestReservedIdentifierMappingIsInjective(t *testing.T) {
	got := []string{ident("class"), ident("class_"), ident("vego_class")}
	want := []string{"vego_class", "class_", "vego_vego_class"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mapped identifiers = %q, want %q", got, want)
		}
	}
}

func TestCxx20TypeAndCastKeywordsAreEscaped(t *testing.T) {
	for _, name := range []string{"char8_t", "char16_t", "char32_t", "const_cast"} {
		if got, want := ident(name), "vego_"+name; got != want {
			t.Fatalf("ident(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReservedPackageNameProducesValidNamespace(t *testing.T) {
	cases := map[string]string{
		"class::vego_part": "vego_class::vego_vego_part",
		"std":              "vego_std",
		"vg":               "vego_vg",
	}
	for namespace, want := range cases {
		g := &gen{p: &compiler.Program{}, hppName: "engine.hpp", ns: namespace}
		header, source := g.files()
		for name, text := range map[string]string{"header": header, "source": source} {
			if !strings.Contains(text, "namespace "+want+" {") {
				t.Fatalf("%s does not escape namespace %q:\n%s", name, namespace, text)
			}
		}
	}
}

func TestRuntimePackageNamespaceCompiles(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("C++ compiler is not installed")
	}
	arena := &compiler.StructDecl{Name: "Arena", Fields: []compiler.Param{{Name: "Value", Type: compiler.TInt}}}
	p := &compiler.Program{Package: "vg", Types: []*compiler.StructDecl{arena},
		StructMap: map[string]*compiler.StructDecl{"Arena": arena}}
	header, source := (&gen{p: p, hppName: "engine.hpp", ns: p.Package}).files()
	dir := t.TempDir()
	files := map[string]string{
		"engine.hpp": header,
		"engine.cpp": source,
		"vg.hpp":     "#pragma once\n#include <cstdint>\nnamespace vg { class Arena {}; }\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(cxx, "-std=c++20", "-Wall", "-Wextra", "-Wpedantic", "-Werror", "-c", "engine.cpp")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C++ compile failed: %v\n%s", err, output)
	}
}

func TestLoopPostPinsImpureAssignmentPlace(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("C++ compiler is not installed")
	}
	mark := &compiler.FuncDecl{Name: "mark"}
	p := &compiler.Program{Funcs: []*compiler.FuncDecl{mark}, FuncMap: map[string]*compiler.FuncDecl{"mark": mark}}
	array := &compiler.Type{K: compiler.KArray, Elem: compiler.TInt,
		ALen: &compiler.Expr{K: "int", Value: "1", Typ: compiler.TInt}}
	call := func(value string) *compiler.Expr {
		return &compiler.Expr{K: "call", Name: "mark", Typ: compiler.TInt,
			Args: []*compiler.Expr{{K: "int", Value: value, Typ: compiler.TInt}}}
	}
	post := &compiler.Stmt{K: "assign",
		Lhs: []*compiler.Expr{{K: "index", Typ: compiler.TInt,
			X: &compiler.Expr{K: "ident", Name: "values", Typ: array}, Index: call("1")}},
		Value: call("2")}
	g := &gen{p: p, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	g.resetNames(g.fn)
	lowered := g.inlineStmt(post)
	source := `#include <array>
#include <cstddef>
#include <cstdint>
namespace vg {
template <typename T, size_t N>
T& at(std::array<T, N>& a, int64_t i) { return a[size_t(i)]; }
}
static int64_t trace;
static int64_t mark(int64_t value) { trace = trace * 10 + value; return value - 1; }
int main() {
    std::array<int64_t, 1> values{};
    ` + lowered + `;
    return trace == 12 ? 0 : 1;
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "post.cpp")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "post")
	cmd := exec.Command(cxx, "-std=c++20", "-Wall", "-Wextra", "-Wpedantic", "-Werror", path, "-o", bin)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C++ compile failed: %v\n%s\n%s", err, output, source)
	}
	if output, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("generated post expression used the wrong evaluation order: %v\n%s\n%s", err, output, source)
	}
}

func TestShiftCountIsCheckedAgainstTheWidth(t *testing.T) {
	f := &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}
	g := &gen{p: &compiler.Program{}, fn: f}
	g.resetNames(f)
	a := &compiler.Expr{K: "ident", Name: "a", Typ: compiler.TU64}
	for _, test := range []struct {
		op    string
		count *compiler.Expr
		want  string
	}{
		{"<<", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TInt}, "(a << vg::shift_count(n, 64))"},
		{">>", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TI32}, "(a >> vg::shift_count(n, 64))"},
		{"<<", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TU8}, "(a << vg::shift_count(n, 64))"},
		{">>", &compiler.Expr{K: "int", Value: "3", Typ: compiler.TInt, IsConst: true}, "(a >> 3LL)"},
	} {
		expr := &compiler.Expr{K: "binary", Op: test.op, X: a, Y: test.count, Typ: a.Typ}
		if got := g.expr(expr); got != test.want {
			t.Errorf("%s by %s = %q, want %q", test.op, test.count.Typ, got, test.want)
		}
	}

	n := &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TInt}
	for _, op := range []string{"<<=", ">>="} {
		s := &compiler.Stmt{K: "op_assign", Op: op,
			Lhs: []*compiler.Expr{{K: "ident", Name: "a", Typ: compiler.TU8}}, Value: n}
		if got, want := g.opAssign(s, "a"), "a "+op+" vg::shift_count(n, 8)"; got != want {
			t.Errorf("%s = %q, want %q", op, got, want)
		}
	}
}

// TestNegativeShiftCountAborts compiles a shift of every integer type by a count of every integer type.
// It builds with NDEBUG, which must not remove the check, and expects a negative count to abort.
func TestNegativeShiftCountAborts(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("C++ compiler is not installed")
	}
	vghpp, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "native", "cpp", "vg.hpp"))
	if err != nil {
		t.Fatal(err)
	}
	p := shiftProgram(t)
	header, source := (&gen{p: p, hppName: "engine.hpp", ns: p.Package}).files()
	main := `#include <cstdio>
#include "engine.hpp"
int main() {
    std::printf("%lld\n", (long long)shifts::shl_int64_int(5, 3));
    std::fflush(stdout);
    std::printf("%lld\n", (long long)shifts::shl_int64_int(5, -1));
    return 0;
}
`
	dir := t.TempDir()
	for name, content := range map[string]string{
		"engine.hpp": header, "engine.cpp": source, "vg.hpp": string(vghpp), "main.cpp": main,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(cxx, "-std=c++20", "-O2", "-fwrapv", "-DNDEBUG", "-Wall", "-Wextra",
		"-Wno-parentheses-equality", "-Werror", "engine.cpp", "main.cpp", "-o", "shifts")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C++ compile failed: %v\n%s", err, output)
	}
	output, err := exec.Command(filepath.Join(dir, "shifts")).CombinedOutput()
	if err == nil || !strings.HasPrefix(string(output), "5\n") || !strings.Contains(string(output), "check failed: n >= 0") {
		t.Fatalf("a negative shift count did not abort after a valid shift: %v\n%s", err, output)
	}
}

// shiftProgram builds a checked Vego program with one function per pair of integer operand and count types.
// shl_X_C(x, n) shifts x left and right by n, through both the compound and the plain operators.
func shiftProgram(t *testing.T) *compiler.Program {
	t.Helper()
	types := []string{"uint8", "uint16", "uint32", "uint64", "int32", "int64", "int"}
	var funcs []string
	for _, x := range types {
		for _, c := range types {
			funcs = append(funcs, fmt.Sprintf(`{"k":"func","name":"shl_%s_%s",
				"params":[{"name":"x","type":{"k":"named","name":"%s"}},{"name":"n","type":{"k":"named","name":"%s"}}],
				"results":[{"k":"named","name":"%s"}],
				"body":[
					{"k":"op_assign","lhs":{"k":"ident","name":"x"},"op":"<<=","value":{"k":"ident","name":"n"}},
					{"k":"op_assign","lhs":{"k":"ident","name":"x"},"op":">>=","value":{"k":"ident","name":"n"}},
					{"k":"return","values":[{"k":"binary","op":">>",
						"x":{"k":"binary","op":"<<","x":{"k":"ident","name":"x"},"y":{"k":"ident","name":"n"}},
						"y":{"k":"ident","name":"n"}}]}]}`, x, c, x, c, x))
		}
	}
	src := `{"vego":1,"package":"shifts","consts":[],"vars":[],"types":[],"funcs":[` + strings.Join(funcs, ",") + `]}`
	p, err := compiler.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.Check(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// A computed array length stays untyped in the IR, so the printer must use the length the checker resolved.
func TestComputedArrayLength(t *testing.T) {
	src := `{"vego":1,"package":"lens","consts":[{"k":"const","name":"n","type":null,"value":{"k":"int","value":"2"}}],
		"vars":[],"types":[],"funcs":[{"k":"func","name":"Main","params":[],"results":[{"k":"named","name":"int64"}],"body":[
			{"k":"var_decl","name":"a","type":{"k":"array","elem":{"k":"named","name":"int64"},
				"len":{"k":"binary","op":"+","x":{"k":"ident","name":"n"},"y":{"k":"int","value":"2"}}},"value":null},
			{"k":"return","values":[{"k":"conv","type":{"k":"named","name":"int64"},
				"x":{"k":"builtin","fn":"len","args":[{"k":"ident","name":"a"}],"spread":false,"type":null}}]}]}]}`
	p, err := compiler.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.Check(p); err != nil {
		t.Fatal(err)
	}
	_, source, err := Emit(p, Options{HeaderName: "engine.hpp"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "std::array<int64_t, 4>") {
		t.Fatalf("the array length did not fold to 4:\n%s", source)
	}
}
