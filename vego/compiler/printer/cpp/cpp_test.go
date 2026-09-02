package cpp

import (
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
