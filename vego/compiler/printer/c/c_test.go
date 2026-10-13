package c

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
	"github.com/oneregex/revera/vego/compiler/export"
)

// Every case builds a program whose names would collide with C, libc, or the printer's own names if they were printed as written.
func TestGeneratedCNamesAreDisjoint(t *testing.T) {
	tests := []struct {
		name   string
		opts   Options
		source string
		main   string
	}{
		{
			name: "generated helpers",
			source: `package vego

type Item struct{ value int }
type i64 struct{ value int64 }
type i64_eq struct{ value int }

func slice_i64() int      { return 7 }
func vego_slice_i64() int { return 8 }
func slice_i64_make() int { return 1 }
func bytes_from_str() int { return 5 }
func Item_eq() int        { return 4 }

func Helpers() int {
	vego_slice_i64 := 9
	values := make([]int, 1)
	var items []i64_eq
	structs := []i64{i64{value: 5}}
	var a Item
	var b Item
	equal := 0
	if a == b {
		equal = Item_eq()
	}
	bytes := []uint8("x")
	return slice_i64() + vego_slice_i64 + slice_i64_make() + len(values) + len(items) + int(structs[0].value) + equal + bytes_from_str() + len(bytes)
}

func index() int { return 0 }

func ContinuePost() int {
	var values [1]int
	i := 0
	saved := 0
	for ; i < 3; values[index()] = i {
		i++
		if i < 3 {
			continue
		}
		saved = values[0]
	}
	return saved*10 + values[0]
}
`,
			main: `#include "engine.h"
int main(void) {
    vg_arena mem = {0};
    int ok = {{Helpers}}(&mem) == 33 && {{vego_slice_i64}}() == 8 && {{ContinuePost}}() == 23;
    vg_arena_free(&mem);
    return ok ? 0 : 1;
}
`,
		},
		{
			name: "C and libc names",
			source: `package vego

var local_vego_char = 5
var x = 5
var vego_tmp_1 = 5

type S struct {
	errno  int
	stdin  int
	size_t int
	EOF    int
}

func touch(s *S) int {
	s.errno++
	return 0
}

func F() int {
	char := 2
	vego_x := 2
	int64_t := 1
	INT64_MAX := 1
	L_tmpnam := 6
	s := S{errno: 1, stdin: 2, size_t: 3, EOF: 4}
	values := [1]int{38}
	values[touch(&s)] += vego_tmp_1
	return char + local_vego_char + vego_x + x + int64_t + INT64_MAX + L_tmpnam + values[0] + s.errno + s.stdin + s.size_t + s.EOF
}
`,
			main: `#include "engine.h"
int main(void) { return {{F}}() == 76 ? 0 : 1; }
`,
		},
		{
			name: "package and function spelling a libc macro",
			source: `package INT64

func MAX() int { return 7 }
`,
			main: `#include "engine.h"
int main(void) { return {{MAX}}() == 7 ? 0 : 1; }
`,
		},
		{
			name: "names spelling the old header guard",
			source: `package P

const ENGINE_H = 7

type S struct{ P_ENGINE_H int }

func F() int {
	a := S{P_ENGINE_H: ENGINE_H}
	b := a
	if a == b {
		return b.P_ENGINE_H
	}
	return 0
}
`,
			main: `#include "engine.h"
int main(void) { return {{F}}() == 7 ? 0 : 1; }
`,
		},
		{
			name: "namespace matching the runtime prefix",
			opts: Options{Prefix: "vg"},
			source: `package p

func check() int { return 7 }
`,
			main: `#include "engine.h"
int main(void) { return {{check}}() == 7 ? 0 : 1; }
`,
		},
		{
			name: "header name with punctuation",
			opts: Options{HeaderName: "engine+v2.h"},
			source: `package p

func F() int { return 7 }
`,
			main: `#include "engine+v2.h"
int main(void) { return {{F}}() == 7 ? 0 : 1; }
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compileAndRun(t, test.source, test.main, test.opts)
		})
	}
}

func compileAndRun(t *testing.T, source, main string, opts Options) {
	t.Helper()
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	blob, violations, err := export.Package(dir)
	if err != nil || len(violations) != 0 {
		t.Fatalf("export: %v %v", err, violations)
	}
	p, err := compiler.Load(blob)
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.Check(p); err != nil {
		t.Fatal(err)
	}
	main = replaceGlobalRefs(main, p, opts)
	header, generated, err := Emit(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	vgh, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "native", "c", "vg.h"))
	if err != nil {
		t.Fatal(err)
	}
	headerName := opts.HeaderName
	if headerName == "" {
		headerName = "engine.h"
	}
	for name, content := range map[string]string{
		headerName: header, "engine.c": generated, "vg.h": string(vgh), "main.c": main,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(cc, "-std=c11", "-fwrapv", "-Wall", "-Wextra", "-Werror", "engine.c", "main.c", "-o", "test")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile generated C: %v\n%s\n%s\n%s", err, out, header, generated)
	}
	if out, err := exec.Command(filepath.Join(dir, "test")).CombinedOutput(); err != nil {
		t.Fatalf("run generated C: %v\n%s", err, out)
	}
}

func replaceGlobalRefs(source string, p *compiler.Program, opts Options) string {
	prefix := p.Package
	if opts.Prefix != "" {
		prefix = opts.Prefix
	}
	for _, f := range p.Funcs {
		source = strings.ReplaceAll(source, "{{"+f.Name+"}}", GlobalName(prefix, f.Name))
	}
	return source
}

func TestTokensAreInjective(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range []string{token("g", "a_b", "c"), token("g", "a", "b_c"), token("t", "a_b", "c"), token("l", "a_b_c")} {
		if seen[name] {
			t.Fatalf("duplicate token %q", name)
		}
		seen[name] = true
	}
	if got, want := token("g", "revera_eng", "Compile"), "vego_g_7265766572615f656e67x_Compile"; got != want {
		t.Fatalf("token = %q, want %q", got, want)
	}
}

func TestSyntheticTempsAvoidProgramNames(t *testing.T) {
	f := &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{"vego_tmp_2": {}}}
	p := &compiler.Program{Package: "vego", ConstMap: map[string]*compiler.ValueDecl{"vego_tmp_1": {Name: "vego_tmp_1"}}}
	g := &gen{p: p, prefix: p.Package, fn: f}
	if got := g.newTmp(); got != "vego_tmp_3" {
		t.Fatalf("first synthetic temporary = %q, want %q", got, "vego_tmp_3")
	}
}

func TestLogicalRightOperandStaysConditional(t *testing.T) {
	// The right operand allocates, so its prelude must run only when the left operand is true.
	sliceInt := &compiler.Type{K: compiler.KSlice, Elem: compiler.TInt}
	f := &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}
	g := &gen{p: &compiler.Program{}, fn: f}
	g.depth = 1
	e := &compiler.Expr{K: "binary", Op: "&&", Typ: compiler.TBool,
		X: &compiler.Expr{K: "ident", Name: "ok", Typ: compiler.TBool},
		Y: &compiler.Expr{K: "binary", Op: ">", Typ: compiler.TBool,
			X: &compiler.Expr{K: "builtin", Name: "len", Typ: compiler.TInt,
				Args: []*compiler.Expr{{K: "builtin", Name: "make", Typ: sliceInt, TypeRef: sliceInt,
					Args: []*compiler.Expr{{K: "call", Name: "n", Typ: compiler.TInt}}}}},
			Y: &compiler.Expr{K: "call", Name: "n", Typ: compiler.TInt}}}
	got := g.expr(e)
	if got != "vego_tmp_1" {
		t.Fatalf("logical expression = %q, want a temporary", got)
	}
	joined := strings.Join(g.pre, "\n")
	if !strings.Contains(joined, "bool vego_tmp_1 = ok;") || !strings.Contains(joined, "if (vego_tmp_1) {") {
		t.Fatalf("prelude does not guard the right operand:\n%s", joined)
	}
}

func TestShiftCountIsCheckedAgainstTheWidth(t *testing.T) {
	f := &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}
	g := &gen{p: &compiler.Program{}, fn: f}
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
	main := fmt.Sprintf(`#include <stdio.h>
#include "engine.h"
int main(void) {
    printf("%%lld\n", (long long)%s(5, 3));
    fflush(stdout);
    printf("%%lld\n", (long long)%s(5, -1));
    return 0;
}
`, token("g", p.Package, "shl_int64_int"), token("g", p.Package, "shl_int64_int"))
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
