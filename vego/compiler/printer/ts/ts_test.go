package ts

import (
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
	"github.com/oneregex/revera/vego/compiler/export"
)

func load(t *testing.T, src string) *compiler.Program {
	t.Helper()
	p, err := compiler.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.Check(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReservedIdentifierMappingIsInjective(t *testing.T) {
	got := []string{ident("new"), ident("new_"), ident("plain")}
	want := []string{"new_", "new__", "plain"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mapped identifiers = %q, want %q", got, want)
		}
	}
	if got := field("clone"); got != "clone_" {
		t.Fatalf("field(clone) = %q, want clone_", got)
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

func TestIntegerTypesMapToNumberAndBigint(t *testing.T) {
	const src = `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
		{"k": "func", "name": "f",
		 "params": [{"name": "a", "type": {"k": "named", "name": "int"}},
		            {"name": "b", "type": {"k": "named", "name": "int64"}},
		            {"name": "c", "type": {"k": "named", "name": "uint32"}}],
		 "results": [{"k": "named", "name": "int64"}],
		 "body": [
			{"k": "define", "names": ["x"], "value": {"k": "binary", "op": "*",
				"x": {"k": "ident", "name": "a"}, "y": {"k": "int", "value": "3"}}},
			{"k": "define", "names": ["y"], "value": {"k": "binary", "op": "*",
				"x": {"k": "ident", "name": "c"}, "y": {"k": "int", "value": "3"}}},
			{"k": "return", "values": [{"k": "binary", "op": "+",
				"x": {"k": "ident", "name": "b"},
				"y": {"k": "conv", "type": {"k": "named", "name": "int64"},
				      "x": {"k": "binary", "op": "+", "x": {"k": "ident", "name": "x"},
				            "y": {"k": "conv", "type": {"k": "named", "name": "int"}, "x": {"k": "ident", "name": "y"}}}}}]}
		 ]}
	]}`
	out, err := Emit(load(t, src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export function f(a: number, b: bigint, c: number): bigint {",
		"const x: number = vg.chk(a * 3);",
		"const y: number = (Math.imul(c, 3) >>> 0);",
		"return BigInt.asIntN(64, b + BigInt(vg.chk(x + y)));",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated output does not contain %q:\n%s", want, out)
		}
	}
}

func TestStructValuesAreClonedWhereGoCopies(t *testing.T) {
	const src = `{"vego": 1, "package": "t", "consts": [], "vars": [],
	 "types": [{"k": "type", "name": "P", "fields": [{"name": "x", "type": {"k": "named", "name": "int32"}}]}],
	 "funcs": [
		{"k": "func", "name": "pick",
		 "params": [{"name": "ps", "type": {"k": "slice", "elem": {"k": "struct_ref", "name": "P"}}},
		            {"name": "q", "type": {"k": "struct_ref", "name": "P"}}],
		 "results": [{"k": "struct_ref", "name": "P"}],
		 "body": [
			{"k": "define", "names": ["a"], "value": {"k": "index", "x": {"k": "ident", "name": "ps"}, "index": {"k": "int", "value": "0"}}},
			{"k": "define", "names": ["b"], "value": {"k": "call", "fn": "pick", "args": [{"k": "ident", "name": "ps"}, {"k": "ident", "name": "a"}]}},
			{"k": "if", "cond": {"k": "binary", "op": "==", "x": {"k": "ident", "name": "b"}, "y": {"k": "ident", "name": "q"}},
			 "then": [{"k": "return", "values": [{"k": "ident", "name": "q"}]}]},
			{"k": "return", "values": [{"k": "ident", "name": "b"}]}
		 ]}
	]}`
	out, err := Emit(load(t, src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"const a: P = ps.buf[ps.off + vg.ix(0, ps.len)].clone();",
		"const b: P = pick(ps, a);",
		"if (P.eq(b, q)) {",
		"return q.clone();",
		"return b;",
		"static readonly elem: vg.Elem<P> = vg.structElem(() => new P(), 4);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated output does not contain %q:\n%s", want, out)
		}
	}
}

func TestBreakInsideSwitchLabelsTheLoop(t *testing.T) {
	const src = `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
		{"k": "func", "name": "f", "params": [{"name": "n", "type": {"k": "named", "name": "int"}}],
		 "results": [{"k": "named", "name": "int"}],
		 "body": [
			{"k": "define", "names": ["i"], "value": {"k": "int", "value": "0"}},
			{"k": "for", "cond": {"k": "binary", "op": "<", "x": {"k": "ident", "name": "i"}, "y": {"k": "ident", "name": "n"}},
			 "body": [
				{"k": "switch", "tag": {"k": "ident", "name": "i"},
				 "cases": [{"values": [{"k": "int", "value": "3"}], "body": [{"k": "break"}]}]},
				{"k": "incdec", "op": "++", "lhs": {"k": "ident", "name": "i"}}
			 ]},
			{"k": "return", "values": [{"k": "ident", "name": "i"}]}
		 ]}
	]}`
	out, err := Emit(load(t, src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"_t1: while ((i < n)) {",
		"case 3:",
		"break _t1;",
		"i = vg.chk(i + 1);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated output does not contain %q:\n%s", want, out)
		}
	}
}

func TestExactNumberAcceptsPowersOfTwoBeyondTheSafeRange(t *testing.T) {
	two62 := new(big.Int).Lsh(big.NewInt(1), 62)
	if !exactNumber(two62) {
		t.Fatal("2^62 should have an exact number form")
	}
	if exactNumber(new(big.Int).Add(two62, big.NewInt(1))) {
		t.Fatal("2^62 + 1 should not have an exact number form")
	}
	if !exactNumber(big.NewInt(-9007199254740991)) {
		t.Fatal("-(2^53 - 1) should have an exact number form")
	}
}

func TestEmitMatchesCheckedInEngine(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	for _, tc := range []struct{ ir, checked string }{
		{filepath.Join(root, "revera.vego.json"), filepath.Join(root, "ts", "src", "engine.ts")},
		{filepath.Join(root, "vego", "probe", "probe.vego.json"), filepath.Join(root, "ts", "src", "probe_engine.ts")},
	} {
		p, err := compiler.LoadFile(tc.ir)
		if err != nil {
			t.Fatal(err)
		}
		out, err := Emit(p)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(tc.checked)
		if err != nil {
			t.Fatal(err)
		}
		if out != string(want) {
			t.Errorf("%s is stale; run make generate GENERATION_TARGETS=ts", tc.checked)
		}
		cmd := exec.Command("node", "--check", tc.checked)
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("node --check %s: %v\n%s", tc.checked, err, msg)
		}
	}
}

func TestKeyedCompositeKeepsSourceOrderWhenValuesHaveEffects(t *testing.T) {
	const src = `{"vego": 1, "package": "t", "consts": [], "vars": [],
	 "types": [{"k": "type", "name": "S", "fields": [
		{"name": "a", "type": {"k": "named", "name": "int"}},
		{"name": "b", "type": {"k": "named", "name": "int"}}]}],
	 "funcs": [
		{"k": "func", "name": "n", "params": [], "results": [{"k": "named", "name": "int"}],
		 "body": [{"k": "return", "values": [{"k": "int", "value": "1"}]}]},
		{"k": "func", "name": "mk", "params": [], "results": [{"k": "struct_ref", "name": "S"}],
		 "body": [{"k": "return", "values": [{"k": "composite", "type": {"k": "struct_ref", "name": "S"},
			"fields": [{"name": "b", "value": {"k": "call", "fn": "n", "args": []}},
			           {"name": "a", "value": {"k": "call", "fn": "n", "args": []}}]}]}]}
	]}`
	out, err := Emit(load(t, src))
	if err != nil {
		t.Fatal(err)
	}
	want := "return (_t1 = n(), _t2 = n(), new S(_t2, _t1));"
	if !strings.Contains(out, want) {
		t.Errorf("generated output does not contain %q:\n%s", want, out)
	}
}

func TestRangeOverArrayValueCopiesTheArray(t *testing.T) {
	const src = `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
		{"k": "func", "name": "f",
		 "params": [{"name": "a", "type": {"k": "array", "len": {"k": "int", "value": "3"}, "elem": {"k": "named", "name": "int32"}}}],
		 "results": [{"k": "named", "name": "int32"}],
		 "body": [
			{"k": "define", "names": ["t"], "value": {"k": "conv", "type": {"k": "named", "name": "int32"}, "x": {"k": "int", "value": "0"}}},
			{"k": "range", "idx": "_", "val": "v", "over": {"k": "ident", "name": "a"},
			 "body": [{"k": "op_assign", "op": "+=", "lhs": {"k": "ident", "name": "t"}, "value": {"k": "ident", "name": "v"}}]},
			{"k": "return", "values": [{"k": "ident", "name": "t"}]}
		 ]}
	]}`
	out, err := Emit(load(t, src))
	if err != nil {
		t.Fatal(err)
	}
	want := "const _t1 = a.slice();"
	if !strings.Contains(out, want) {
		t.Errorf("generated output does not contain %q:\n%s", want, out)
	}
}

// emitSample exports a Go package and prints it as TypeScript.
func emitSample(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	blob, violations, err := export.Package(dir)
	if err != nil || len(violations) != 0 {
		t.Fatalf("export: %v %v", err, violations)
	}
	out, err := Emit(load(t, string(blob)))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// runSample prints a Go package as TypeScript and runs it with node against the real runtime.
// Every function whose name starts with T takes no argument and returns an integer.
// The result holds one "Name = value" line per such function, in declaration order.
func runSample(t *testing.T, source string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	engine := emitSample(t, source)
	runtime, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "ts", "src", "vg.ts"))
	if err != nil {
		t.Fatal(err)
	}
	var main strings.Builder
	main.WriteString("import * as e from \"./engine.ts\";\n")
	for _, line := range strings.Split(engine, "\n") {
		name, ok := strings.CutPrefix(line, "export function T")
		if !ok {
			continue
		}
		name = "T" + name[:strings.Index(name, "(")]
		main.WriteString("console.log(\"" + name + " = \" + String(e." + name + "()));\n")
	}
	dir := t.TempDir()
	for name, content := range map[string]string{
		"engine.ts":    engine,
		"vg.ts":        string(runtime),
		"main.ts":      main.String(),
		"package.json": "{\"type\": \"module\"}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, "--no-warnings", "main.ts")
	cmd.Dir = dir
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s\n%s", err, got, engine)
	}
	return string(got)
}

func wantAll(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("generated output does not contain %q:\n%s", w, out)
		}
	}
}

// aliasSample writes to storage that is also reachable under another name: through a pointer, a view, a slice or a global.
// The expected values come from go run.
const aliasSample = `package sample

type In struct {
	A   int64
	Arr [2]int64
}

type Out struct {
	N int64
	P In
}

var table = In{A: 1}

var row = [3]int32{1, 2, 3}

func readAfterWrite(v Out, p *Out) int64 {
	p.N = 5
	p.P.Arr[0] = 6
	return v.N*10 + v.P.Arr[0]
}

func TValueParam() int64 {
	var o Out
	o.N = 1
	o.P.Arr[0] = 2
	return readAfterWrite(o, &o)
}

func TOtherBorrow() int64 {
	var a Out
	a.N = 3
	var o Out
	return readAfterWrite(a, &o)
}

func readAfterSliceWrite(v In, s []In) int64 {
	s[0].A = 9
	return v.A
}

func TValueParamSlice() int64 {
	s := make([]In, 1)
	s[0].A = 4
	return readAfterSliceWrite(s[0], s)
}

func getRow() [3]int32 {
	return row
}

func getTable() In {
	return table
}

func TGlobalReturn() int64 {
	r := getRow()
	r[0] = 9
	t := getTable()
	t.A = 7
	return int64(row[0])*10 + int64(getRow()[0]) + getTable().A*100
}

func dup() (In, In) {
	var x In
	x.A = 1
	return x, x
}

func TDupReturn() int64 {
	a, b := dup()
	a.A = 5
	return b.A
}

func setThenWrite(p *In, q *Out) int64 {
	q.P = In{A: 1}
	p.A = 5
	return q.P.A
}

func TBorrowedStore() int64 {
	var o Out
	return setThenWrite(&o.P, &o)
}

func TViewedStore() int64 {
	var a [2]int64
	v := a[:]
	a = [2]int64{7, 8}
	s := make([]In, 1)
	w := s[0].Arr[:]
	s[0] = In{Arr: [2]int64{4, 6}}
	return v[0]*10 + v[1] + w[0]*1000 + w[1]*100
}

func TLocalStore() int64 {
	var x In
	x = In{A: 2}
	return x.A
}

func TCopyView() int64 {
	d := make([]In, 1)
	src := make([]In, 1)
	src[0].Arr[1] = 3
	v := d[0].Arr[:]
	copy(d, src)
	return v[1]
}
`

// constSample holds constant expressions whose exact value differs from what evaluating them in the context type would give.
const constSample = `package sample

const neg = -5

func id8(x uint8) uint8 {
	return x
}

func TConstNot() int64 {
	a := uint64(^uint32(0))
	b := int64(^uint8(0))
	return int64(a%100000) + b*100000
}

func TUntyped() int64 {
	var d int32 = (1 << 40) >> 20
	f := id8(3) * ((512 - 2) / 2 / 64)
	return int64(d)*100 + int64(f)
}

func TNegConst() int64 {
	var y int64 = -neg
	return y
}
`

func TestValueArgumentIsCopiedWhenTheCallCanWriteIt(t *testing.T) {
	out := emitSample(t, aliasSample)
	wantAll(t, out,
		"return readAfterWrite(o.clone(), o);",
		"return readAfterSliceWrite(s.buf[s.off + vg.ix(0, s.len)].clone(), s);",
		// A pointer to another variable can't reach the argument.
		"return readAfterWrite(a, o);",
	)
}

func TestReturnedGlobalsAndRepeatedLocalsAreCopied(t *testing.T) {
	out := emitSample(t, aliasSample)
	wantAll(t, out, "return row.slice();", "return table.clone();", "return [x, x.clone()];")
}

func TestStructGlobalFollowsItsClass(t *testing.T) {
	out := emitSample(t, aliasSample)
	class, global := strings.Index(out, "export class In {"), strings.Index(out, "export const table: In = new In(1n);")
	if class < 0 || global < 0 || global < class {
		t.Errorf("the global must follow the class it constructs:\n%s", out)
	}
}

func TestStoreThatAnAliasCanReachCopiesIntoTheObject(t *testing.T) {
	out := emitSample(t, aliasSample)
	wantAll(t, out,
		"set(src_: In): void {",
		"this.Arr.set(src_.Arr);",
		"q.P.set(new In(1n));",
		"a.set(new BigInt64Array([7n, 8n]));",
		"s.buf[s.off + vg.ix(0, s.len)].set(new In(0n, new BigInt64Array([4n, 6n])));",
		// Nothing views x, so the store can replace its object.
		"x = new In(2n);",
	)
}

func TestConstantComplementKeepsTheOperandWidth(t *testing.T) {
	wantAll(t, emitSample(t, constSample), "const a: bigint = 4294967295n;", "const b: bigint = 255n;")
}

func TestUntypedConstantExpressionFoldsExactly(t *testing.T) {
	wantAll(t, emitSample(t, constSample), "const d: number = 1048576;", "const f: number = ((id8(3) * 3) & 0xff);")
}

func TestNegatedConstantFoldsToALiteral(t *testing.T) {
	wantAll(t, emitSample(t, constSample), "const y: bigint = 5n;")
}

func TestAliasAndConstantSamplesMatchGo(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{aliasSample, "TValueParam = 12\nTOtherBorrow = 30\nTValueParamSlice = 4\nTGlobalReturn = 111\nTDupReturn = 1\n" +
			"TBorrowedStore = 5\nTViewedStore = 4678\nTLocalStore = 2\nTCopyView = 3\n"},
		{constSample, "TConstNot = 25567295\nTUntyped = 104857609\nTNegConst = 5\n"},
	} {
		if got := runSample(t, tc.source); got != tc.want {
			t.Errorf("node output:\n%s\nwant:\n%s", got, tc.want)
		}
	}
}
