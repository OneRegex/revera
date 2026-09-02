package ts

import (
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
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
