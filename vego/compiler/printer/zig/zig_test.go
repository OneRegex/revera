package zig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
)

func TestAllocatingFunctionsPropagateOutOfMemory(t *testing.T) {
	const src = `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
		{"k": "func", "name": "f", "params": [],
		 "results": [{"k": "slice", "elem": {"k": "named", "name": "uint8"}}],
		 "body": [{"k": "return", "values": [
			{"k": "builtin", "fn": "make", "args": [{"k": "int", "value": "1"}],
			 "type": {"k": "slice", "elem": {"k": "named", "name": "uint8"}}}
		 ]}]},
		{"k": "func", "name": "g", "params": [],
		 "results": [{"k": "slice", "elem": {"k": "named", "name": "uint8"}}],
		 "body": [{"k": "return", "values": [{"k": "call", "fn": "f", "args": []}]}]}
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
		"pub fn f(mem: vg.Allocator) vg.Allocator.Error!vg.Slice(u8)",
		"return try vg.make(mem, u8, 1);",
		"pub fn g(mem: vg.Allocator) vg.Allocator.Error!vg.Slice(u8)",
		"return try f(mem);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated output does not contain %q:\n%s", want, out)
		}
	}
}

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

func TestImpureDivisionAssignmentPinsPlace(t *testing.T) {
	zig, err := exec.LookPath("zig")
	if err != nil {
		t.Skip("Zig is not installed")
	}
	mark := &compiler.FuncDecl{Name: "mark"}
	p := &compiler.Program{Funcs: []*compiler.FuncDecl{mark}, FuncMap: map[string]*compiler.FuncDecl{"mark": mark}}
	array := &compiler.Type{K: compiler.KArray, Elem: compiler.TInt,
		ALen: &compiler.Expr{K: "int", Value: "1", Typ: compiler.TInt}}
	call := func(value string) *compiler.Expr {
		return &compiler.Expr{K: "call", Name: "mark", Typ: compiler.TInt,
			Args: []*compiler.Expr{{K: "int", Value: value, Typ: compiler.TInt}}}
	}
	assign := &compiler.Stmt{K: "op_assign", Op: "/=",
		Lhs: []*compiler.Expr{{K: "index", Typ: compiler.TInt,
			X: &compiler.Expr{K: "ident", Name: "values", Typ: array}, Index: call("1")}},
		Value: call("2")}
	g := &gen{p: p, fn: &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{}}}
	g.resetNames(g.fn)
	g.stmt(assign, 1)
	ordinary := g.b.String()
	post := g.inlineStmt(assign)
	source := `const vg = struct {
    fn divT(x: i64, y: i64) i64 { return @divTrunc(x, y); }
};
var trace: i64 = 0;
fn mark(value: i64) i64 { trace = trace * 10 + value; return value - 1; }
pub fn main() !void {
    var values = [_]i64{8};
` + ordinary + `
    if (trace != 12) return error.BadOrdinaryOrder;
    trace = 0;
    var i: i64 = 0;
    while (i < 1) : (` + post + `) { i += 1; }
    if (trace != 12) return error.BadPostOrder;
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "main.zig")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(zig, "run", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Zig failed: %v\n%s\n%s", err, output, source)
	}
}
