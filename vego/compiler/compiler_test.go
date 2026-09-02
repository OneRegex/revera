package compiler

import (
	"os"
	"strings"
	"testing"
)

func loadEngine(t *testing.T) *Program {
	t.Helper()
	blob, err := os.ReadFile("../../revera.vego.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(blob)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// load runs the full front end over a small JSON program.
func load(src string) (*Program, error) {
	p, err := Load([]byte(src))
	if err != nil {
		return nil, err
	}
	if err := Check(p); err != nil {
		return nil, err
	}
	return p, nil
}

// TestAllocates pins the contract between the Allocates analysis and the printers.
// Every form a printer renders with the "mem" context must set the flag, directly and through a call.
func TestAllocates(t *testing.T) {
	const su8 = `{"k": "slice", "elem": {"k": "named", "name": "uint8"}}`
	const str = `{"k": "named", "name": "string"}`
	cases := []struct {
		name string
		body string
		res  string
		want bool
	}{
		{"make", `{"k": "builtin", "fn": "make", "args": [{"k": "int", "value": "1"}], "type": ` + su8 + `}`, su8, true},
		{"append", `{"k": "builtin", "fn": "append", "args": [{"k": "ident", "name": "b"}, {"k": "int", "value": "1"}]}`, su8, true},
		{"conv_to_str", `{"k": "conv", "type": ` + str + `, "x": {"k": "ident", "name": "b"}}`, str, true},
		{"conv_to_bytes", `{"k": "conv", "type": ` + su8 + `, "x": {"k": "conv", "type": ` + str + `, "x": {"k": "ident", "name": "b"}}}`, su8, true},
		{"slice_literal", `{"k": "composite", "type": ` + su8 + `, "elems": [{"k": "int", "value": "1"}]}`, su8, true},
		{"pure", `{"k": "slice_expr", "x": {"k": "ident", "name": "b"}, "lo": null, "hi": {"k": "int", "value": "0"}}`, su8, false},
	}
	for _, tc := range cases {
		src := `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
			{"k": "func", "name": "f", "params": [{"name": "b", "type": ` + su8 + `}],
			 "results": [` + tc.res + `],
			 "body": [{"k": "return", "values": [` + tc.body + `]}]},
			{"k": "func", "name": "g", "params": [{"name": "b", "type": ` + su8 + `}],
			 "results": [` + tc.res + `],
			 "body": [{"k": "return", "values": [{"k": "call", "fn": "f", "args": [{"k": "ident", "name": "b"}]}]}]}
		]}`
		p, err := load(src)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := p.FuncMap["f"].Allocates; got != tc.want {
			t.Errorf("%s: f.Allocates = %v, want %v", tc.name, got, tc.want)
		}
		if got := p.FuncMap["g"].Allocates; got != tc.want {
			t.Errorf("%s: g.Allocates = %v, want %v (transitive)", tc.name, got, tc.want)
		}
		if p.CalleeAllocates("f") != tc.want || p.CalleeAllocates("missing") {
			t.Errorf("%s: CalleeAllocates disagrees with the flag", tc.name)
		}
	}
}

func TestReservedPackageName(t *testing.T) {
	src := `{"vego": 1, "package": "t", "consts": [
		{"k": "const", "name": "mem", "type": null, "value": {"k": "int", "value": "1"}}
	], "vars": [], "types": [], "funcs": []}`
	if _, err := load(src); err == nil {
		t.Fatal("package-level name mem was not rejected")
	}
}

func TestReservedTupleNamespace(t *testing.T) {
	src := `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [
		{"k": "type", "name": "Tup_i64_i64", "fields": []}
	], "funcs": []}`
	if _, err := load(src); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("tuple namespace declaration error = %v, want a reserved-name error", err)
	}
}

func TestAllocatingGlobalInitializer(t *testing.T) {
	src := `{"vego": 1, "package": "t", "consts": [], "vars": [
		{"k": "var", "name": "s", "type": {"k": "named", "name": "string"},
		 "value": {"k": "conv", "type": {"k": "named", "name": "string"},
		           "x": {"k": "composite", "type": {"k": "slice", "elem": {"k": "named", "name": "uint8"}},
		                 "elems": [{"k": "int", "value": "65"}]}}}
	], "types": [], "funcs": []}`
	if _, err := load(src); err == nil {
		t.Fatal("allocating global initializer was not rejected")
	}
}

func TestLoadRejectsInvalidDocuments(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"missing fields", `{}`, "version"},
		{"unsupported version", `{"vego": 2, "package": "t", "consts": [], "vars": [], "types": [], "funcs": []}`, "unsupported"},
		{"malformed list", `{"vego": 1, "package": "t", "consts": {}, "vars": [], "types": [], "funcs": []}`, "consts"},
		{"malformed declaration", `{"vego": 1, "package": "t", "consts": [null], "vars": [], "types": [], "funcs": []}`, "invalid Vego JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load([]byte(tc.src)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestRejectsUnsupportedSourceSemantics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"pointer comparison",
			`{"vego": 1, "package": "t", "consts": [], "vars": [],
			  "types": [{"k": "type", "name": "S", "fields": []}],
			  "funcs": [{"k": "func", "name": "f",
			    "params": [{"name": "a", "type": {"k": "ptr", "name": "S"}}, {"name": "b", "type": {"k": "ptr", "name": "S"}}],
			    "results": [{"k": "named", "name": "bool"}],
			    "body": [{"k": "return", "values": [{"k": "binary", "op": "==", "x": {"k": "ident", "name": "a"}, "y": {"k": "ident", "name": "b"}}]}]}]}`,
			"pointer comparisons",
		},
		{
			"string addition",
			`{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [],
			  "funcs": [{"k": "func", "name": "f",
			    "params": [{"name": "a", "type": {"k": "named", "name": "string"}}, {"name": "b", "type": {"k": "named", "name": "string"}}],
			    "results": [{"k": "named", "name": "string"}],
			    "body": [{"k": "return", "values": [{"k": "binary", "op": "+", "x": {"k": "ident", "name": "a"}, "y": {"k": "ident", "name": "b"}}]}]}]}`,
			"string operators",
		},
		{
			"string switch",
			`{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [],
			  "funcs": [{"k": "func", "name": "f",
			    "params": [{"name": "s", "type": {"k": "named", "name": "string"}}], "results": [],
			    "body": [{"k": "switch", "tag": {"k": "ident", "name": "s"},
			      "cases": [{"values": [{"k": "str", "value": "x"}], "body": []}], "default": null}]}]}`,
			"switch tag",
		},
		{
			"int64 range count",
			`{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [],
			  "funcs": [{"k": "func", "name": "f",
			    "params": [{"name": "n", "type": {"k": "named", "name": "int64"}}], "results": [],
			    "body": [{"k": "range", "idx": null, "val": null, "over": {"k": "ident", "name": "n"}, "body": []}]}]}`,
			"range over unsupported type",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := load(tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("load error = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestAcceptsIntRangeCount(t *testing.T) {
	src := `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [],
		"funcs": [{"k": "func", "name": "f",
		  "params": [{"name": "n", "type": {"k": "named", "name": "int"}}], "results": [],
		  "body": [{"k": "range", "idx": null, "val": null, "over": {"k": "ident", "name": "n"}, "body": []}]}]}`
	if _, err := load(src); err != nil {
		t.Fatal(err)
	}
}

func TestMutatedParameterShadowAvoidsPackageNames(t *testing.T) {
	src := `{"vego": 1, "package": "t", "consts": [
		{"k": "const", "name": "x_v", "type": null, "value": {"k": "int", "value": "10"}}
	], "vars": [], "types": [], "funcs": [
		{"k": "func", "name": "Sum", "params": [{"name": "x", "type": {"k": "named", "name": "int"}}],
		 "results": [{"k": "named", "name": "int"}], "body": [
			{"k": "incdec", "op": "++", "lhs": {"k": "ident", "name": "x"}},
			{"k": "return", "values": [{"k": "binary", "op": "+", "x": {"k": "ident", "name": "x"}, "y": {"k": "ident", "name": "x_v"}}]}
		]}
	]}`
	p, err := load(src)
	if err != nil {
		t.Fatal(err)
	}
	f := p.FuncMap["Sum"]
	if len(f.Body) != 3 || f.Body[0].K != "define" {
		t.Fatalf("mutated parameter was not shadowed: %#v", f.Body)
	}
	shadow := f.Body[0].Names[0]
	if shadow != "x_vv" {
		t.Fatalf("shadow name = %q, want x_vv", shadow)
	}
	ret := f.Body[2].Values[0]
	if ret.X.Name != shadow || ret.Y.Name != "x_v" {
		t.Fatalf("return operands = %q and %q, want %q and package constant x_v", ret.X.Name, ret.Y.Name, shadow)
	}
}

func TestIntegerConstantRepresentability(t *testing.T) {
	oversized := `{"vego": 1, "package": "t", "consts": [
		{"k": "const", "name": "huge", "type": null,
		 "value": {"k": "binary", "op": "<<", "x": {"k": "int", "value": "1"}, "y": {"k": "int", "value": "63"}}}
	], "vars": [], "types": [], "funcs": []}`
	if _, err := load(oversized); err == nil || !strings.Contains(err.Error(), "does not fit int") {
		t.Fatalf("oversized constant error = %v, want an int representability error", err)
	}

	accepted := `{"vego": 1, "package": "t", "consts": [
		{"k": "const", "name": "maxInt", "type": null,
		 "value": {"k": "binary", "op": "-",
		   "x": {"k": "binary", "op": "<<", "x": {"k": "int", "value": "1"}, "y": {"k": "int", "value": "63"}},
		   "y": {"k": "int", "value": "1"}}},
		{"k": "const", "name": "highBit", "type": {"k": "named", "name": "uint64"},
		 "value": {"k": "binary", "op": "<<", "x": {"k": "int", "value": "1"}, "y": {"k": "int", "value": "63"}}}
	], "vars": [], "types": [], "funcs": []}`
	p, err := load(accepted)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.ConstMap["maxInt"].ConstVal.String(); got != "9223372036854775807" {
		t.Fatalf("maxInt = %s", got)
	}
	if got := p.ConstMap["highBit"].ConstVal.String(); got != "9223372036854775808" {
		t.Fatalf("highBit = %s", got)
	}
}

func TestTupleNamesAreTotalAndInjective(t *testing.T) {
	array := func(n int64, elem *Type) *Type {
		return &Type{K: KArray, ALenVal: n, ALenSet: true, Elem: elem}
	}
	types := []*Type{
		TBool, TU8, TU16, TU32, TU64, TI32, TI64, TInt, TStr,
		{K: KSlice, Elem: TU8},
		{K: KSlice, Elem: &Type{K: KStruct, Name: "u8"}},
		array(2, TU8), array(12, TU8), array(2, &Type{K: KSlice, Elem: TU8}),
		{K: KStruct, Name: "u8"},
		{K: KStruct, Name: "su8"},
		{K: KStruct, Name: "A_B"},
		{K: KStruct, Name: "A"},
		{K: KStruct, Name: "B_bool"},
	}

	mangled := map[string]*Type{}
	for _, typ := range types {
		name := Mangle(typ)
		if prior := mangled[name]; prior != nil && !Same(prior, typ) {
			t.Fatalf("Mangle collision: %s and %s both map to %q", prior, typ, name)
		}
		mangled[name] = typ
	}

	tupleNames := map[string][2]*Type{}
	for _, a := range types {
		for _, b := range types {
			pair := [2]*Type{a, b}
			name := TupName(pair)
			if prior, ok := tupleNames[name]; ok && (!Same(prior[0], a) || !Same(prior[1], b)) {
				t.Fatalf("TupName collision: (%s, %s) and (%s, %s) both map to %q", prior[0], prior[1], a, b, name)
			}
			tupleNames[name] = pair
		}
	}
}

func TestCheckEngine(t *testing.T) {
	p := loadEngine(t)
	if len(p.Funcs) == 0 || len(p.Types) == 0 {
		t.Fatal("empty program")
	}
	// Every expression that a printer needs a type for must have one.
	// This walk checks all of them.
	for _, f := range p.Funcs {
		WalkBody(f.Body, func(e *Expr) {
			if e.Typ == nil && e.K != "call" && e.K != "builtin" {
				t.Errorf("untyped %s node (name=%q op=%q)", e.K, e.Name, e.Op)
			}
			if e.Untyped {
				t.Errorf("expression left untyped-constant: %s %q %q", e.K, e.Name, e.Value)
			}
		}, nil)
	}
}
