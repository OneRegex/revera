package rust

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
)

func TestSyntheticTempsAvoidProgramNames(t *testing.T) {
	f := &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{"_t1": {}}}
	g := &gen{p: &compiler.Program{Funcs: []*compiler.FuncDecl{f}}}
	g.resetNames(f)
	if got := g.newTmp(); got != "_t2" {
		t.Fatalf("first synthetic temporary = %q, want %q", got, "_t2")
	}
}

func TestReservedRustKeywordsUseRawIdentifiers(t *testing.T) {
	for _, name := range []string{"abstract", "become"} {
		if got, want := ident(name), "r#"+name; got != want {
			t.Fatalf("ident(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestStructEqualityHelperAvoidsProgramName(t *testing.T) {
	const src = `{"vego":1,"package":"collision","consts":[],"vars":[],"types":[
		{"k":"type","name":"S","fields":[{"name":"X","type":{"k":"named","name":"int"}}]}
	],"funcs":[
		{"k":"func","name":"vego_eq_S","params":[
			{"name":"left","type":{"k":"struct_ref","name":"S"}},
			{"name":"right","type":{"k":"struct_ref","name":"S"}}],
		 "results":[{"k":"named","name":"bool"}],
		 "body":[{"k":"return","values":[{"k":"bool","value":true}]}]},
		{"k":"func","name":"equal","params":[
			{"name":"left","type":{"k":"struct_ref","name":"S"}},
			{"name":"right","type":{"k":"struct_ref","name":"S"}}],
		 "results":[{"k":"named","name":"bool"}],
		 "body":[{"k":"return","values":[{"k":"binary","op":"==",
			"x":{"k":"ident","name":"left"},"y":{"k":"ident","name":"right"}}]}]}
	]}`
	p, err := compiler.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.Check(p); err != nil {
		t.Fatal(err)
	}
	out := (&gen{p: p}).file()
	for _, want := range []string{
		"pub fn vego_eq_S(a: S, b: S)",
		"pub fn vego_vego_eq_S(left: S, right: S)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated output does not contain %q:\n%s", want, out)
		}
	}

	files := map[string]string{"engine.rs": out, "lib.rs": "mod vg {}\nmod engine;\n"}
	if _, output, err := compileRust(t, files, "--crate-type=lib", "lib.rs"); err != nil {
		t.Fatalf("rustc failed: %v\n%s", err, output)
	}
}

func TestGeneratedFileForbidsUnsafeCode(t *testing.T) {
	out := (&gen{p: &compiler.Program{}}).file()
	if !strings.Contains(out, "#![forbid(unsafe_code)]") {
		t.Fatalf("generated output does not forbid unsafe code:\n%s", out)
	}
	if strings.Contains(out, "unsafe {") {
		t.Fatalf("generated output contains an unsafe block:\n%s", out)
	}
}

func TestSliceAssignmentsUseSafePinnedSlots(t *testing.T) {
	mark := &compiler.FuncDecl{Name: "mark"}
	p := &compiler.Program{
		Funcs:   []*compiler.FuncDecl{mark},
		FuncMap: map[string]*compiler.FuncDecl{"mark": mark},
	}
	call := func(value string) *compiler.Expr {
		return &compiler.Expr{K: "call", Name: "mark", Typ: compiler.TInt,
			Args: []*compiler.Expr{{K: "int", Value: value, Typ: compiler.TInt}}}
	}
	entry := &compiler.Type{K: compiler.KStruct, Name: "Entry"}
	entries := &compiler.Type{K: compiler.KSlice, Elem: entry}
	slot := &compiler.Expr{K: "index", Typ: entry,
		X: &compiler.Expr{K: "ident", Name: "entries", Typ: entries}, Index: call("1")}
	field := &compiler.Expr{K: "field", Name: "value", Typ: compiler.TInt, X: slot}

	g := &gen{p: p, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	g.resetNames(g.fn)
	g.assignLine(0, field, g.expr(call("2")))
	out := g.b.String()
	assertInOrder(t, out,
		"let _t1 = entries.slot(mark(1i64));",
		"let _t2 = mark(2i64);",
		"_t1.update(_t2, |mut _t3, _t2| { _t3.value = _t2; _t3 });")
	for _, bad := range []string{"unsafe {", "addr_of_mut", ".ptr("} {
		if strings.Contains(out, bad) {
			t.Fatalf("safe assignment contains %q:\n%s", bad, out)
		}
	}
}

func TestSliceOperationAssignmentUsesOwnedValue(t *testing.T) {
	mark := &compiler.FuncDecl{Name: "mark"}
	p := &compiler.Program{
		Funcs:   []*compiler.FuncDecl{mark},
		FuncMap: map[string]*compiler.FuncDecl{"mark": mark},
	}
	call := func(value string) *compiler.Expr {
		return &compiler.Expr{K: "call", Name: "mark", Typ: compiler.TInt,
			Args: []*compiler.Expr{{K: "int", Value: value, Typ: compiler.TInt}}}
	}
	values := &compiler.Type{K: compiler.KSlice, Elem: compiler.TInt}
	lhs := &compiler.Expr{K: "index", Typ: compiler.TInt,
		X: &compiler.Expr{K: "ident", Name: "values", Typ: values}, Index: call("1")}

	g := &gen{p: p, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	g.resetNames(g.fn)
	g.stmt(&compiler.Stmt{K: "op_assign", Op: "+=", Lhs: []*compiler.Expr{lhs}, Value: call("2")}, 0)
	out := g.b.String()
	assertInOrder(t, out,
		"let _t1 = values.slot(mark(1i64));",
		"let _t2 = mark(2i64);",
		"_t1.update(_t2, |mut _t3, _t2| { _t3 = (_t3).wrapping_add(_t2); _t3 });")
	for _, bad := range []string{"unsafe {", "addr_of_mut", ".ptr("} {
		if strings.Contains(out, bad) {
			t.Fatalf("safe operation assignment contains %q:\n%s", bad, out)
		}
	}
}

func TestArithmeticUsesWrappingOperations(t *testing.T) {
	g := &gen{p: &compiler.Program{}, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	for _, test := range []struct {
		op   string
		typ  *compiler.Type
		want string
	}{
		{"+", compiler.TI64, "(a).wrapping_add(b)"},
		{"-", compiler.TI64, "(a).wrapping_sub(b)"},
		{"*", compiler.TU64, "(a).wrapping_mul(b)"},
		{"/", compiler.TI64, "(a).wrapping_div(b)"},
		{"%", compiler.TI64, "(a).wrapping_rem(b)"},
		{"/", compiler.TU64, "(a / b)"},
	} {
		a := &compiler.Expr{K: "ident", Name: "a", Typ: test.typ}
		b := &compiler.Expr{K: "ident", Name: "b", Typ: test.typ}
		expr := &compiler.Expr{K: "binary", Op: test.op, X: a, Y: b, Typ: test.typ}
		if got := g.binary(expr); got != test.want {
			t.Errorf("%s expression = %q, want %q", test.op, got, test.want)
		}
	}

	g.stmt(&compiler.Stmt{
		K: "op_assign", Op: "*=",
		Lhs:   []*compiler.Expr{{K: "ident", Name: "a", Typ: compiler.TU64}},
		Value: &compiler.Expr{K: "ident", Name: "b", Typ: compiler.TU64},
	}, 0)
	if got, want := g.b.String(), "a = (a).wrapping_mul(b);\n"; got != want {
		t.Fatalf("multiplication assignment = %q, want %q", got, want)
	}
}

func TestShiftCountIsCheckedAgainstTheWidth(t *testing.T) {
	g := &gen{p: &compiler.Program{}, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	a := &compiler.Expr{K: "ident", Name: "a", Typ: compiler.TU64}
	for _, test := range []struct {
		op    string
		count *compiler.Expr
		want  string
	}{
		{"<<", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TInt}, "(a << vg::shift_count(n, 64))"},
		{">>", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TI32}, "(a >> vg::shift_count(n, 64))"},
		{"<<", &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TU8}, "(a << vg::shift_count(n, 64))"},
		{">>", &compiler.Expr{K: "int", Value: "3", Typ: compiler.TInt, IsConst: true}, "(a >> 3i64)"},
	} {
		expr := &compiler.Expr{K: "binary", Op: test.op, X: a, Y: test.count, Typ: a.Typ}
		if got := g.binary(expr); got != test.want {
			t.Errorf("%s by %s = %q, want %q", test.op, test.count.Typ, got, test.want)
		}
	}

	g.stmt(&compiler.Stmt{
		K: "op_assign", Op: ">>=",
		Lhs:   []*compiler.Expr{{K: "ident", Name: "a", Typ: compiler.TU8}},
		Value: &compiler.Expr{K: "ident", Name: "n", Typ: compiler.TI64},
	}, 0)
	if got, want := g.b.String(), "a >>= vg::shift_count(n, 8);\n"; got != want {
		t.Fatalf("shift assignment = %q, want %q", got, want)
	}
}

// TestShiftsCompileForEveryCountType compiles a shift of every integer type by a count of every integer type.
// It then runs a negative count without overflow checks, as the release profile does, and expects a panic.
func TestShiftsCompileForEveryCountType(t *testing.T) {
	out := (&gen{p: shiftProgram(t)}).file()
	main := `mod vg;
mod engine;
fn main() {
    println!("{}", engine::shl_int64_int(5, 3));
    println!("{}", engine::shl_int64_int(5, -1));
}
`
	files := map[string]string{"engine.rs": out, "vg.rs": vgRuntime(t), "main.rs": main}
	dir, output, err := compileRust(t, files, "-O", "-C", "overflow-checks=off", "-A", "warnings", "main.rs", "-o", "shifts")
	if err != nil {
		t.Fatalf("rustc failed: %v\n%s", err, output)
	}
	output, err = exec.Command(filepath.Join(dir, "shifts")).CombinedOutput()
	if err == nil || !strings.HasPrefix(string(output), "5\n") || !strings.Contains(string(output), "shift count out of range") {
		t.Fatalf("a negative shift count did not panic after a valid shift: %v\n%s", err, output)
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

func TestImpureArrayAssignmentChecksIndexBeforeValue(t *testing.T) {
	mark := &compiler.FuncDecl{Name: "mark"}
	p := &compiler.Program{
		Funcs:   []*compiler.FuncDecl{mark},
		FuncMap: map[string]*compiler.FuncDecl{"mark": mark},
	}
	call := func(value string) *compiler.Expr {
		return &compiler.Expr{K: "call", Name: "mark", Typ: compiler.TInt,
			Args: []*compiler.Expr{{K: "int", Value: value, Typ: compiler.TInt}}}
	}
	length := &compiler.Expr{K: "int", Value: "4", Typ: compiler.TInt}
	array := &compiler.Type{K: compiler.KArray, Elem: compiler.TInt, ALen: length}
	lhs := &compiler.Expr{K: "index", Typ: compiler.TInt,
		X: &compiler.Expr{K: "ident", Name: "values", Typ: array}, Index: call("1")}

	g := &gen{p: p, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	g.resetNames(g.fn)
	g.assignLine(0, lhs, g.expr(call("2")))
	out := g.b.String()
	assertInOrder(t, out,
		"let _t1 = vg::array_index(mark(1i64), ((4) as usize));",
		"let _t2 = mark(2i64);",
		"values[_t1] = _t2;")
}

func TestArrayProjectionIsResolvedBeforeSlotUpdate(t *testing.T) {
	mark := &compiler.FuncDecl{Name: "mark"}
	p := &compiler.Program{
		Funcs:   []*compiler.FuncDecl{mark},
		FuncMap: map[string]*compiler.FuncDecl{"mark": mark},
	}
	length := &compiler.Expr{K: "int", Value: "4", Typ: compiler.TInt}
	array := &compiler.Type{K: compiler.KArray, Elem: compiler.TInt, ALen: length}
	entry := &compiler.Type{K: compiler.KStruct, Name: "Entry"}
	entries := &compiler.Type{K: compiler.KSlice, Elem: entry}
	slot := &compiler.Expr{K: "index", Typ: entry,
		X:     &compiler.Expr{K: "ident", Name: "entries", Typ: entries},
		Index: &compiler.Expr{K: "int", Value: "0", Typ: compiler.TInt}}
	field := &compiler.Expr{K: "field", Name: "items", Typ: array, X: slot}
	lhs := &compiler.Expr{K: "index", Typ: compiler.TInt, X: field,
		Index: &compiler.Expr{K: "ident", Name: "j", Typ: compiler.TInt}}
	rhs := &compiler.Expr{K: "call", Name: "mark", Typ: compiler.TInt,
		Args: []*compiler.Expr{{K: "int", Value: "3", Typ: compiler.TInt}}}

	g := &gen{p: p, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	g.resetNames(g.fn)
	g.assignLine(0, lhs, g.expr(rhs))
	out := g.b.String()
	assertInOrder(t, out,
		"let _t1 = entries.slot(0i64);",
		"let _t2 = vg::array_index(j, ((4) as usize));",
		"let _t3 = mark(3i64);",
		"_t1.update(_t3, |mut _t4, _t3| { _t4.items[_t2] = _t3; _t4 });")
}

func TestArraySubscriptsAreChecked(t *testing.T) {
	length := &compiler.Expr{K: "int", Value: "4", Typ: compiler.TInt}
	array := &compiler.Type{K: compiler.KArray, Elem: compiler.TInt, ALen: length}
	values := &compiler.Expr{K: "ident", Name: "values", Typ: array}
	read := &compiler.Expr{K: "index", Typ: compiler.TInt, X: values,
		Index: &compiler.Expr{K: "ident", Name: "k", Typ: compiler.TU8}}
	lhs := &compiler.Expr{K: "index", Typ: compiler.TInt, X: values,
		Index: &compiler.Expr{K: "ident", Name: "j", Typ: compiler.TInt}}

	g := &gen{p: &compiler.Program{}, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	g.resetNames(g.fn)
	if got, want := g.expr(read), "values[vg::array_index(((k) as i64), ((4) as usize))]"; got != want {
		t.Errorf("read: got %s, want %s", got, want)
	}
	g.assignLine(0, lhs, "7i64")
	if got, want := g.b.String(), "values[vg::array_index(j, ((4) as usize))] = 7i64;\n"; got != want {
		t.Errorf("assignment: got %q, want %q", got, want)
	}
}

// An array type of 2^32 elements must not compile for a 32-bit target.
func TestArrayLengthPastUsizeFailsToCompile(t *testing.T) {
	const target = "i686-unknown-linux-musl"
	sysroot, err := exec.Command("rustc", "--print", "sysroot").Output()
	if err != nil {
		t.Skip("rustc is not installed")
	}
	if _, err := os.Stat(filepath.Join(strings.TrimSpace(string(sysroot)), "lib", "rustlib", target)); err != nil {
		t.Skip("the " + target + " standard library is not installed")
	}
	length := &compiler.Expr{K: "ident", Name: "N", Typ: compiler.TI64}
	typ := (&gen{p: &compiler.Program{}}).typ(&compiler.Type{K: compiler.KArray, Elem: compiler.TU8, ALen: length})
	for n, ok := range map[string]bool{"4": true, "4294967296": false} {
		lib := "mod vg;\nconst N: i64 = " + n + ";\npub fn f(_a: " + typ + ") {}\n"
		files := map[string]string{"lib.rs": lib, "vg.rs": vgRuntime(t)}
		_, output, err := compileRust(t, files, "--crate-type=lib", "--target", target, "-A", "warnings", "lib.rs")
		if ok != (err == nil) || !ok && !strings.Contains(string(output), "array length out of range") {
			t.Fatalf("length %s: %v\n%s", n, err, output)
		}
	}
}

// compileRust writes files to a fresh directory and runs rustc on them there.
// It skips the test when rustc is not installed.
func compileRust(t *testing.T, files map[string]string, args ...string) (string, []byte, error) {
	t.Helper()
	rustc, err := exec.LookPath("rustc")
	if err != nil {
		t.Skip("rustc is not installed")
	}
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(rustc, append([]string{"--edition=2021"}, args...)...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return dir, output, err
}

func vgRuntime(t *testing.T) string {
	t.Helper()
	vgrs, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "rust", "src", "vg.rs"))
	if err != nil {
		t.Fatal(err)
	}
	return string(vgrs)
}

func assertInOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	at := 0
	for _, part := range parts {
		next := strings.Index(text[at:], part)
		if next < 0 {
			t.Fatalf("output does not contain %q after byte %d:\n%s", part, at, text)
		}
		at += next + len(part)
	}
}
