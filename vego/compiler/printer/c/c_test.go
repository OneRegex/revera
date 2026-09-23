package c

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
)

func TestReservedIdentifierMappingIsInjective(t *testing.T) {
	got := []string{ident("int"), ident("int_"), ident("vego_int")}
	want := []string{"vego_int", "int_", "vego_vego_int"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mapped identifiers = %q, want %q", got, want)
		}
	}
}

func TestRuntimeAndContextNamesAreEscaped(t *testing.T) {
	for _, name := range []string{"mem", "restrict", "errno", "vg_str", "bool"} {
		if got, want := ident(name), "vego_"+name; got != want {
			t.Fatalf("ident(%q) = %q, want %q", name, got, want)
		}
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

func TestLogicalRightOperandStaysConditional(t *testing.T) {
	// The right operand allocates, so its prelude must run only when the left operand is true.
	sliceInt := &compiler.Type{K: compiler.KSlice, Elem: compiler.TInt}
	f := &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}
	g := &gen{p: &compiler.Program{}, fn: f}
	g.resetNames(f)
	g.depth = 1
	e := &compiler.Expr{K: "binary", Op: "&&", Typ: compiler.TBool,
		X: &compiler.Expr{K: "ident", Name: "ok", Typ: compiler.TBool},
		Y: &compiler.Expr{K: "binary", Op: ">", Typ: compiler.TBool,
			X: &compiler.Expr{K: "builtin", Name: "len", Typ: compiler.TInt,
				Args: []*compiler.Expr{{K: "builtin", Name: "make", Typ: sliceInt, TypeRef: sliceInt,
					Args: []*compiler.Expr{{K: "call", Name: "n", Typ: compiler.TInt}}}}},
			Y: &compiler.Expr{K: "call", Name: "n", Typ: compiler.TInt}}}
	got := g.expr(e)
	if got != "_t1" {
		t.Fatalf("logical expression = %q, want a temporary", got)
	}
	joined := strings.Join(g.pre, "\n")
	if !strings.Contains(joined, "bool _t1 = ok;") || !strings.Contains(joined, "if (_t1) {") {
		t.Fatalf("prelude does not guard the right operand:\n%s", joined)
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
		{"<<", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TInt}, "(a << vg_shift_count(n, 64))"},
		{">>", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TI32}, "(a >> vg_shift_count(n, 64))"},
		{"<<", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TU8}, "(a << vg_shift_count(n, 64))"},
		{">>", &compiler.Expr{K: "int", Value: "3", Typ: compiler.TInt, IsConst: true}, "(a >> 3LL)"},
		{">>", &compiler.Expr{K: "int", Value: "64", Typ: compiler.TInt, IsConst: true}, "(a >> vg_shift_count(64LL, 64))"},
	} {
		expr := &compiler.Expr{K: "binary", Op: test.op, X: a, Y: test.count, Typ: a.Typ}
		if got := g.expr(expr); got != test.want {
			t.Errorf("%s by %s = %q, want %q", test.op, test.count.Typ, got, test.want)
		}
	}

	n := &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TInt}
	for _, test := range []struct {
		op   string
		typ  *compiler.Type
		want string
	}{
		{">>=", compiler.TU8, "a >>= vg_shift_count(n, 8)"},
		{"<<=", compiler.TI64, "a = (int64_t)((uint64_t)(a) << (vg_shift_count(n, 64)))"},
	} {
		s := &compiler.Stmt{K: "op_assign", Op: test.op,
			Lhs: []*compiler.Expr{{K: "ident", Name: "a", Typ: test.typ}}, Value: n}
		if got := g.opAssign(s, "a", "n"); got != test.want {
			t.Errorf("%s on %s = %q, want %q", test.op, test.typ, got, test.want)
		}
	}
}

// TestNegativeShiftCountAborts compiles a shift of every integer type by a count of every integer type.
// It builds with NDEBUG, which must not remove the check, and expects a negative count to abort.
func TestNegativeShiftCountAborts(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is not installed")
	}
	vgh, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "native", "c", "vg.h"))
	if err != nil {
		t.Fatal(err)
	}
	p := shiftProgram(t)
	header, source := (&gen{p: p, hdrName: "engine.h", prefix: p.Package}).files()
	main := `#include <stdio.h>
#include "engine.h"
int main(void) {
    printf("%lld\n", (long long)shifts_shl_int64_int(5, 3));
    fflush(stdout);
    printf("%lld\n", (long long)shifts_shl_int64_int(5, -1));
    return 0;
}
`
	dir := t.TempDir()
	for name, content := range map[string]string{
		"engine.h": header, "engine.c": source, "vg.h": string(vgh), "main.c": main,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(cc, "-std=c11", "-O2", "-fwrapv", "-DNDEBUG", "-Wall", "-Wextra",
		"-Wno-parentheses-equality", "-Werror", "engine.c", "main.c", "-o", "shifts")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C compile failed: %v\n%s", err, output)
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

// TestGeneratedSourcesCompile runs the printer over both real Vego programs and compiles the output.
// It uses the same flags as the native/c Makefile, with warnings as errors.
func TestGeneratedSourcesCompile(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is not installed")
	}
	repo, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	vgh, err := os.ReadFile(filepath.Join(repo, "native", "c", "vg.h"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		json   string
		prefix string
	}{
		{json: "revera.vego.json", prefix: "revera_eng"},
		{json: "vego/probe/probe.vego.json", prefix: ""},
	}
	for _, tc := range cases {
		t.Run(tc.json, func(t *testing.T) {
			p, err := compiler.LoadFile(filepath.Join(repo, filepath.FromSlash(tc.json)))
			if err != nil {
				t.Fatal(err)
			}
			g := &gen{p: p, hdrName: "engine.h", prefix: p.Package}
			if tc.prefix != "" {
				g.prefix = tc.prefix
			}
			header, source := g.files()
			dir := t.TempDir()
			for name, content := range map[string]string{
				"engine.h": header, "engine.c": source, "vg.h": string(vgh),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(cc, "-std=c11", "-fwrapv", "-Wall", "-Wextra",
				"-Wno-parentheses-equality", "-Werror", "-c", "engine.c", "-o", os.DevNull)
			cmd.Dir = dir
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("C compile failed: %v\n%s", err, output)
			}
		})
	}
}
