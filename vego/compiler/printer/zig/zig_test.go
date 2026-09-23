package zig

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

// runSample exports a Go package, prints it as Zig, and runs it against the real runtime.
// Every function whose name starts with T takes no argument and returns an integer.
// The result holds one "Name = value" line per such function, in declaration order.
func runSample(t *testing.T, source string) string {
	t.Helper()
	zig, err := exec.LookPath("zig")
	if err != nil {
		t.Skip("Zig is not installed")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sample.go", source)
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
	engine, err := Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	write("engine.zig", engine)
	runtime, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "zig", "src", "vg.zig"))
	if err != nil {
		t.Fatal(err)
	}
	write("vg.zig", string(runtime))
	var main strings.Builder
	main.WriteString("const std = @import(\"std\");\nconst e = @import(\"engine.zig\");\n")
	main.WriteString("pub fn main() !void {\n    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);\n    defer arena.deinit();\n    const mem = arena.allocator();\n    _ = &mem;\n")
	for _, f := range p.Funcs {
		if !strings.HasPrefix(f.Name, "T") {
			continue
		}
		call := "e." + f.Name + "()"
		if f.Allocates {
			call = "try e." + f.Name + "(mem)"
		}
		fmt.Fprintf(&main, "    std.debug.print(\"%s = {d}\\n\", .{%s});\n", f.Name, call)
	}
	main.WriteString("}\n")
	write("main.zig", main.String())
	cmd := exec.Command(zig, "run", "main.zig")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zig run: %v\n%s\n%s", err, out, engine)
	}
	return string(out)
}

func expectLines(t *testing.T, got string, want ...string) {
	t.Helper()
	if strings.TrimSpace(got) != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

// Zig narrows the result of @min and @max when an operand is comptime-known, but Go keeps the operand type.
// Every operand below is comptime-known in Zig, and the result still has to wrap at the Go width.
func TestMinMaxKeepGoType(t *testing.T) {
	const source = `package sample

type Limits struct {
	Lo  int64
	Cap uint64
}

var table = [2]uint64{255, 7}

func triple() [3]int64 {
	return [3]int64{1, 2, 3}
}

func addMinLocal(w uint64) uint64 {
	y := uint64(255)
	return min(w, y) + 1
}

func subMaxLocal(w int64) int64 {
	lo := int64(0)
	return max(w, lo) - 1
}

func mulMinLocal(w uint32) uint32 {
	c := uint32(16)
	return min(w, c) * 20
}

func addMinTable(w uint64) uint64 {
	return min(w, table[0]) + 1
}

func addMinField(w uint64) uint64 {
	l := Limits{Lo: 0, Cap: 255}
	return min(w, l.Cap) + 1
}

func notMaxField(w int64) int64 {
	l := Limits{Lo: 0, Cap: 1}
	return ^max(w, l.Lo)
}

func subMaxLen(w int64) int64 {
	return max(w, int64(len(triple()))) - 10
}

func switchMin(w uint64) int64 {
	one := uint64(1)
	switch min(w, one) {
	case 0:
		return 10
	case 1:
		return 20
	}
	return 30
}

func TMinLocal() int64    { return int64(addMinLocal(1000)) }
func TMaxLocal() int64    { return subMaxLocal(-5) }
func TMinMul() int64      { return int64(mulMinLocal(100)) }
func TMinTable() int64    { return int64(addMinTable(1000)) }
func TMinField() int64    { return int64(addMinField(1000)) }
func TNotMaxField() int64 { return notMaxField(5) }
func TMaxLen() int64      { return subMaxLen(0) }
func TSwitchMin() int64   { return switchMin(7) }
`
	expectLines(t, runSample(t, source),
		"TMinLocal = 256",
		"TMaxLocal = -1",
		"TMinMul = 320",
		"TMinTable = 256",
		"TMinField = 256",
		"TNotMaxField = -6",
		"TMaxLen = -7",
		"TSwitchMin = 20",
	)
}

// In Zig, a try prefix applies to the whole suffix chain, and a composite literal can't take a suffix.
func TestAllocatingAndLiteralOperands(t *testing.T) {
	const source = `package sample

type P struct {
	A int64
	B []int64
}

type Counter struct {
	N int64
}

func mkSlice(n int) []int64 {
	return make([]int64, n)
}

func mkP() P {
	return P{A: 7, B: make([]int64, 3)}
}

func one(c *Counter) int64 {
	c.N++
	return c.N
}

func TLenOfCall() int64 {
	return int64(len(mkSlice(4)))
}

func TFieldOfCall() int64 {
	return mkP().A + int64(len(mkP().B))
}

func TIndexOfConv() int64 {
	s := "hello"
	return int64([]uint8(s)[1])*1000 + int64(len([]uint8(s)[1:3]))
}

func TByteOfConv() int64 {
	b := []uint8{65, 66}
	return int64(string(b)[1])
}

func TIndexOfLiteral() int64 {
	i := 2
	return []int64{4, 5, 6}[i]*100 + [3]int64{1, 2, 3}[i]*10 + P{A: 3}.A + P{}.A
}

func TSliceOfCall() int64 {
	return int64(len(mkSlice(5)[2:]))*10 + int64(cap(make([]int64, 2, 5)))
}

func TIndexOfAppend() int64 {
	var s []int64
	var c Counter
	return append(s, 9)[0]*10 + append(s, one(&c), one(&c))[1]
}

func TNilOfAllocation() int64 {
	r := int64(0)
	if []uint8("") == nil {
		r += 1
	}
	if []uint8{} == nil {
		r += 10
	}
	if nil == mkSlice(0) {
		r += 100
	}
	return r
}
`
	expectLines(t, runSample(t, source),
		"TLenOfCall = 4",
		"TFieldOfCall = 10",
		"TIndexOfConv = 101002",
		"TByteOfConv = 66",
		"TIndexOfLiteral = 633",
		"TSliceOfCall = 35",
		"TIndexOfAppend = 92",
		"TNilOfAllocation = 0",
	)
}

// Go accepts duplicate, exhaustive and non-constant bool cases, but a Zig switch rejects all three.
// In Zig, an integer switch whose cases cover every value of its type can't have an else prong.
func TestSwitchShapes(t *testing.T) {
	var spread strings.Builder
	for v := range 256 {
		fmt.Fprintf(&spread, "\tcase %d:\n\t\tr = %d\n", v, v%7)
	}
	source := `package sample

const on = true

func pick(b bool) int64 {
	switch b {
	case true:
		return 1
	case false:
		return 2
	}
	return 3
}

func pickDup(b bool) int64 {
	switch b {
	case true:
		return 100
	case on:
		return 200
	}
	return 300
}

func pickExpr(b bool) int64 {
	switch b {
	case 1 < 2, "a" > "b":
		return 1000
	default:
		return 2000
	}
}

func pickEmpty(b bool) int64 {
	switch b {
	}
	switch b {
	default:
		if b {
			return 6
		}
		return 7
	}
}

func spread(x uint8) int64 {
	r := int64(-1)
	switch x {
` + spread.String() + `	}
	return r
}

func spreadDefault(x uint8) int64 {
	r := int64(-1)
	hits := int64(0)
	switch x {
` + spread.String() + `	default:
		hits++
	}
	return r + hits*100
}

func TBool() int64      { return pick(true)*10 + pick(false) }
func TBoolDup() int64   { return pickDup(true) + pickDup(false) }
func TBoolExpr() int64  { return pickExpr(true) + pickExpr(false) }
func TBoolEmpty() int64 { return pickEmpty(true)*10 + pickEmpty(false) }
func TSpread() int64    { return spread(200)*10 + spreadDefault(255) }
`
	expectLines(t, runSample(t, source),
		"TBool = 12",
		"TBoolDup = 400",
		"TBoolExpr = 2000",
		"TBoolEmpty = 67",
		"TSpread = 43",
	)
}

// Zig has no ~ on a comptime_int, and no == on arrays.
func TestComplementAndArrayEquality(t *testing.T) {
	const source = `package sample

type P struct {
	S string
}

type Q struct {
	N int64
}

const mask = 3

func TArrayEq() int64 {
	x := [3]int32{1, 2, 3}
	y := [3]int32{1, 2, 3}
	z := [3]int32{1, 2, 4}
	r := int64(0)
	if x == y {
		r += 1
	}
	if x != z {
		r += 10
	}
	if [2]string{"a", "b"} == [2]string{"a", "b"} {
		r += 100
	}
	if [1]P{P{S: "a"}} == [1]P{P{S: "b"}} {
		r += 1000
	}
	if [2][2]uint8{[2]uint8{1, 2}, [2]uint8{3, 4}} == [2][2]uint8{[2]uint8{1, 2}, [2]uint8{3, 4}} {
		r += 10000
	}
	eq := Q{} == Q{N: 0}
	if eq {
		r += 100000
	}
	return r
}

func TComplement() int64 {
	x := 13
	y := int64(-1)
	x &^= 4
	var u uint8 = 0xff
	u &^= 0x0f
	return int64(x&^1)*1000000 + int64(x & ^3)*10000 + (y^^mask)*1000 + int64(u&^0x30)
}
`
	expectLines(t, runSample(t, source),
		"TArrayEq = 110111",
		"TComplement = 8083192",
	)
}

// A range over an array without a value variable only uses the constant length.
// The operand is still evaluated once, and Zig would reject the unused temporary holding it.
func TestRangeOverArrayWithoutValue(t *testing.T) {
	const source = `package sample

type Holder struct {
	arr [3]int64
	n   int
}

func grow(h *Holder) [3]int64 {
	h.n++
	return h.arr
}

func TRange() int64 {
	var h Holder
	for range grow(&h) {
	}
	c := int64(0)
	for i := range grow(&h) {
		c += int64(i)
	}
	arr := [2]int64{5, 6}
	for range arr {
		c += 100
	}
	return c*100 + int64(h.n)*10 + arr[1]
}
`
	expectLines(t, runSample(t, source), "TRange = 20326")
}

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

// An array length such as n + 2 reaches the printer untyped, so every site prints the folded length.
func TestComputedArrayLength(t *testing.T) {
	const source = `package sample

const n = 2

type Box struct {
	V [n * 2]int64
}

func fill(b Box) [n + 2]int64 {
	b.V[3] = 5
	return b.V
}

func TArrayLength() int64 {
	var a [n + 2]int64
	a[3] = 5
	return a[3] + int64(len(a))
}

func TArrayLengthEverywhere() int64 {
	var b Box
	c := [n + 2]int64{1, 2}
	t := int64(0)
	for i, v := range fill(b) {
		t += int64(i) * v
	}
	s := c[:]
	return t*1000 + int64(len(fill(b)))*100 + int64(len(s))*10 + c[0] + c[3]
}
`
	expectLines(t, runSample(t, source),
		"TArrayLength = 9",
		"TArrayLengthEverywhere = 15441",
	)
}

// Constants stay comptime values whose Zig type mirrors the Go type, typed or untyped.
// An untyped constant built with min, max, ^ or &^ must still fit a narrower use.
func TestConstantTyping(t *testing.T) {
	const source = `package sample

const lowest = min(3, 5)

const flipped = ^3

const cleared = 13 &^ 4

const negative = -16

const small = min(uint8(1), uint8(2)) + 200

const flippedByte = ^uint8(3)

const clearedByte = uint8(13) &^ 4

func TUntypedConstants() int64 {
	var x uint8 = 250
	var z int32 = 7
	a := x + lowest
	b := z & flipped
	c := x & cleared
	d := x & ^negative
	return int64(a)*1000000 + int64(b)*10000 + int64(c)*100 + int64(d)
}

func TTypedConstants() int64 {
	var x uint8 = 255
	return int64(small)*1000000 + int64(x&flippedByte)*1000 + int64(clearedByte)
}

func TInlineConstants() int64 {
	var x uint8 = 240
	var y int64 = 100
	return int64(x&^0x30)*1000000 + int64(max(uint8(7), 9)+x)*1000 + (y&^cleared)*10 + int64(complementOfMin())
}

func complementOfMin() int32 {
	return int32(^min(2, 3))
}
`
	expectLines(t, runSample(t, source),
		"TUntypedConstants = 253040810",
		"TTypedConstants = 201252009",
		"TInlineConstants = 192249997",
	)
}
