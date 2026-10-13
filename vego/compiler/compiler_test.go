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
		{"make", `{"k": "builtin", "fn": "make", "args": [{"k": "int", "value": "1"}], "spread": false, "type": ` + su8 + `}`, su8, true},
		{"append", `{"k": "builtin", "fn": "append", "args": [{"k": "ident", "name": "b"}, {"k": "int", "value": "1"}], "spread": false}`, su8, true},
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
	src = `{"vego": 1, "package": "vg", "consts": [], "vars": [], "types": [], "funcs": []}`
	if _, err := load(src); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved package name error = %v", err)
	}
}

func TestBlankIdentifierCannotDeclareNames(t *testing.T) {
	const src = `{"vego": 1, "package": "PackageName",
		"consts": [{"k": "const", "name": "ConstName", "type": null, "value": {"k": "int", "value": "1"}}],
		"vars": [{"k": "var", "name": "VarName", "type": null, "value": {"k": "int", "value": "1"}}],
		"types": [{"k": "type", "name": "TypeName", "fields": [
			{"name": "FieldName", "type": {"k": "named", "name": "int"}}]}],
		"funcs": [{"k": "func", "name": "FuncName",
			"params": [{"name": "ParamName", "type": {"k": "named", "name": "int"}}], "results": [],
			"body": [
				{"k": "var_decl", "name": "LocalVarName", "type": {"k": "named", "name": "int"}, "value": null},
				{"k": "define", "names": ["LocalDefineName"], "value": {"k": "int", "value": "1"}}
			]}]}`
	for _, name := range []string{
		"PackageName", "ConstName", "VarName", "TypeName", "FieldName", "FuncName", "ParamName", "LocalVarName", "LocalDefineName",
	} {
		if _, err := load(strings.Replace(src, name, "_", 1)); err == nil || !strings.Contains(err.Error(), "blank identifier") {
			t.Errorf("%s: error = %v, want a blank identifier error", name, err)
		}
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
		{"noncanonical integer", `{"vego": 1, "package": "t", "consts": [{"name": "n", "type": null, "value": {"k": "int", "value": "01"}}], "vars": [], "types": [], "funcs": []}`, "invalid integer value"},
		{"negative integer literal", `{"vego": 1, "package": "t", "consts": [{"name": "n", "type": null, "value": {"k": "int", "value": "-1"}}], "vars": [], "types": [], "funcs": []}`, "invalid integer value"},
		{"integer beyond uint64", `{"vego": 1, "package": "t", "consts": [{"name": "n", "type": null, "value": {"k": "int", "value": "18446744073709551616"}}], "vars": [], "types": [], "funcs": []}`, "outside uint64"},
		{"null function body", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [], "results": [], "body": null}]}`, "function body is null"},
		{"range value without index", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [], "results": [], "body": [{"k": "range", "idx": null, "val": "v", "over": {"k": "int", "value": "1"}, "body": []}]}]}`, "requires an index"},
		{"empty range index", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [], "results": [], "body": [{"k": "range", "idx": "", "val": null, "over": {"k": "int", "value": "1"}, "body": []}]}]}`, "index variable is empty"},
		{"empty range value", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [], "results": [], "body": [{"k": "range", "idx": "i", "val": "", "over": {"k": "int", "value": "1"}, "body": []}]}]}`, "value variable is empty"},
		{"blank range index", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [], "results": [], "body": [{"k": "range", "idx": "_", "val": null, "over": {"k": "int", "value": "1"}, "body": []}]}]}`, "has no variables"},
		{"two blank range variables", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [{"name": "s", "type": {"k": "slice", "elem": {"k": "named", "name": "int"}}}], "results": [], "body": [{"k": "range", "idx": "_", "val": "_", "over": {"k": "ident", "name": "s"}, "body": []}]}]}`, "has no variables"},
		{"empty switch case", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [], "results": [], "body": [{"k": "switch", "tag": {"k": "bool", "value": true}, "cases": [{"values": [], "body": []}], "default": null}]}]}`, "case has no values"},
		{"missing builtin spread", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [], "results": [], "body": [{"k": "expr_stmt", "value": {"k": "builtin", "fn": "copy", "args": []}}]}]}`, "spread must be a bool"},
		{"non-boolean builtin spread", `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [{"name": "f", "params": [], "results": [], "body": [{"k": "expr_stmt", "value": {"k": "builtin", "fn": "copy", "args": [], "spread": 0}}]}]}`, "spread must be a bool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load([]byte(tc.src)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want an error containing %q", err, tc.want)
			}
		})
	}
	if _, err := Load([]byte("{\xff}")); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
}

func TestLoadNormalizesIncDecOperators(t *testing.T) {
	program := func(op string) string {
		return `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
			{"k": "func", "name": "f", "params": [{"name": "n", "type": {"k": "named", "name": "int"}}],
			 "results": [], "body": [{"k": "incdec", "op": "` + op + `", "lhs": {"k": "ident", "name": "n"}}]}]}`
	}
	for op, want := range map[string]string{"++": "+=", "--": "-="} {
		p, err := Load([]byte(program(op)))
		if err != nil {
			t.Errorf("%s: %v", op, err)
			continue
		}
		if got := p.Funcs[0].Body[0].Op; got != want {
			t.Errorf("%s normalized to %q, want %q", op, got, want)
		}
	}
	if _, err := Load([]byte(program("wat"))); err == nil || !strings.Contains(err.Error(), "unknown increment") {
		t.Fatalf("unknown operator error = %v", err)
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
		{
			"blank loop post target",
			`{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
			  {"k": "func", "name": "bump", "params": [], "results": [{"k": "named", "name": "int"}],
			   "body": [{"k": "return", "values": [{"k": "int", "value": "1"}]}]},
			  {"k": "func", "name": "f", "params": [], "results": [], "body": [
			   {"k": "for", "init": null, "cond": {"k": "bool", "value": false},
			    "post": {"k": "assign", "lhs": [{"k": "ident", "name": "_"}],
			             "value": {"k": "call", "fn": "bump", "args": []}}, "body": []}]}]}`,
			"blank identifier cannot be a loop post target",
		},
		{
			"loop init assignment",
			`{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
			  {"k": "func", "name": "f", "params": [{"name": "n", "type": {"k": "named", "name": "int"}}],
			   "results": [], "body": [{"k": "for",
			    "init": {"k": "assign", "lhs": [{"k": "ident", "name": "n"}], "value": {"k": "int", "value": "0"}},
			    "cond": {"k": "bool", "value": false}, "post": null, "body": []}]}]}`,
			"loop init must declare one variable",
		},
		{
			"loop post declaration",
			`{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
			  {"k": "func", "name": "f", "params": [], "results": [], "body": [{"k": "for",
			    "init": null, "cond": {"k": "bool", "value": false},
			    "post": {"k": "define", "names": ["n"], "value": {"k": "int", "value": "1"}}, "body": []}]}]}`,
			"loop post statement is not in the subset",
		},
		{
			"non-call expression statement",
			`{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
			  {"k": "func", "name": "f", "params": [], "results": [],
			   "body": [{"k": "expr_stmt", "value": {"k": "int", "value": "1"}}]}]}`,
			"expression statements must be calls",
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

func TestRejectsInvalidCheckedIR(t *testing.T) {
	program := func(consts, vars, types, funcs string) string {
		return `{"vego":1,"package":"t","consts":` + consts + `,"vars":` + vars + `,"types":` + types + `,"funcs":` + funcs + `}`
	}
	intType := `{"k":"named","name":"int"}`
	boolType := `{"k":"named","name":"bool"}`
	u8Type := `{"k":"named","name":"uint8"}`
	i32Type := `{"k":"named","name":"int32"}`
	intLit := func(n string) string { return `{"k":"int","value":"` + n + `"}` }
	conv := func(typ, value string) string { return `{"k":"conv","type":` + typ + `,"x":` + value + `}` }
	fn := func(params, results, body string) string {
		return `[{"name":"f","params":` + params + `,"results":` + results + `,"body":` + body + `}]`
	}
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"duplicate package declaration",
			program(`[{"name":"n","type":null,"value":`+intLit("1")+`}]`, `[{"name":"n","type":null,"value":`+intLit("2")+`}]`, `[]`, `[]`),
			"redeclares",
		},
		{
			"recursive value type",
			program(`[]`, `[]`, `[{"name":"S","fields":[{"name":"next","type":{"k":"struct_ref","name":"S"}}]}]`, `[]`),
			"recursive value type",
		},
		{
			"constant cycle",
			program(`[{"name":"a","type":null,"value":{"k":"ident","name":"b"}},{"name":"b","type":null,"value":{"k":"ident","name":"a"}}]`, `[]`, `[]`, `[]`),
			"initialization cycle",
		},
		{
			"nonconstant global",
			program(`[]`, `[{"name":"a","type":null,"value":`+intLit("1")+`},{"name":"b","type":null,"value":{"k":"ident","name":"a"}}]`, `[]`, `[]`),
			"non-constant initializer",
		},
		{
			"invalid scalar conversion",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[`+intType+`]`, `[{"k":"return","values":[{"k":"conv","type":`+intType+`,"x":{"k":"bool","value":true}}]}]`)),
			"conversion outside the subset",
		},
		{
			"ordered booleans",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[`+boolType+`]`, `[{"k":"return","values":[{"k":"binary","op":"<","x":{"k":"bool","value":false},"y":{"k":"bool","value":true}}]}]`)),
			"ordered comparison",
		},
		{
			"slice equality",
			program(`[]`, `[]`, `[]`, fn(`[{"name":"a","type":{"k":"slice","elem":`+intType+`}},{"name":"b","type":{"k":"slice","elem":`+intType+`}}]`, `[`+boolType+`]`, `[{"k":"return","values":[{"k":"binary","op":"==","x":{"k":"ident","name":"a"},"y":{"k":"ident","name":"b"}}]}]`)),
			"slices can only be compared with nil",
		},
		{
			"missing return",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[`+intType+`]`, `[]`)),
			"without returning",
		},
		{
			"parameter redeclaration",
			program(`[]`, `[]`, `[]`, fn(`[{"name":"x","type":`+intType+`}]`, `[]`, `[{"k":"var_decl","name":"x","type":`+intType+`,"value":null}]`)),
			"redeclared in the same scope",
		},
		{
			"break in switch",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[]`, `[{"k":"switch","tag":{"k":"bool","value":true},"cases":[{"values":[{"k":"bool","value":true}],"body":[{"k":"break"}]}],"default":null}]`)),
			"break must target a loop",
		},
		{
			"nonconstant switch case",
			program(`[]`, `[]`, `[]`, fn(`[{"name":"n","type":`+intType+`}]`, `[]`, `[{"k":"switch","tag":{"k":"ident","name":"n"},"cases":[{"values":[{"k":"ident","name":"n"}],"body":[]}],"default":null}]`)),
			"case is not constant",
		},
		{
			"range value containing a slice",
			program(`[]`, `[]`, `[]`, fn(`[{"name":"rows","type":{"k":"slice","elem":{"k":"slice","elem":`+intType+`}}}]`, `[]`, `[{"k":"range","idx":"i","val":"row","over":{"k":"ident","name":"rows"},"body":[]}]`)),
			"containing a slice",
		},
		{
			"slice of global array",
			program(`[]`, `[{"name":"a","type":{"k":"array","len":`+intLit("1")+`,"elem":`+intType+`},"value":{"k":"composite","type":{"k":"array","len":`+intLit("1")+`,"elem":`+intType+`},"elems":[]}}]`, `[]`, fn(`[]`, `[]`, `[{"k":"assign","lhs":[{"k":"ident","name":"_"}],"value":{"k":"slice_expr","x":{"k":"ident","name":"a"},"lo":null,"hi":null}}]`)),
			"cannot be sliced",
		},
		{
			"too many array elements",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[]`, `[{"k":"define","names":["a"],"value":{"k":"composite","type":{"k":"array","len":`+intLit("1")+`,"elem":`+intType+`},"elems":[`+intLit("1")+`,`+intLit("2")+`]}}]`)),
			"too many values",
		},
		{
			"negative make size",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[]`, `[{"k":"define","names":["s"],"value":{"k":"builtin","fn":"make","spread":false,"type":{"k":"slice","elem":`+intType+`},"args":[{"k":"unary","op":"-","x":`+intLit("1")+`}]}}]`)),
			"make size is negative",
		},
		{
			"constant array index",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[`+intType+`]`, `[{"k":"return","values":[{"k":"index","x":{"k":"composite","type":{"k":"array","len":`+intLit("1")+`,"elem":`+intType+`},"elems":[]},"index":`+intLit("1")+`}]}]`)),
			"out of bounds",
		},
		{
			"unaddressable array slice",
			program(`[]`, `[]`, `[]`, `[{"name":"array","params":[],"results":[{"k":"array","len":`+intLit("1")+`,"elem":`+intType+`}],"body":[{"k":"return","values":[{"k":"composite","type":{"k":"array","len":`+intLit("1")+`,"elem":`+intType+`},"elems":[]}]}]},{"name":"f","params":[],"results":[],"body":[{"k":"assign","lhs":[{"k":"ident","name":"_"}],"value":{"k":"slice_expr","x":{"k":"call","fn":"array","args":[]},"lo":null,"hi":null}}]}]`),
			"unaddressable array",
		},
		{
			"constant zero divisor",
			program(`[]`, `[]`, `[]`, fn(`[{"name":"n","type":`+intType+`}]`, `[`+intType+`]`, `[{"k":"return","values":[{"k":"binary","op":"/","x":{"k":"ident","name":"n"},"y":`+intLit("0")+`}]}]`)),
			"division by zero",
		},
		{
			"typed addition overflow",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[`+u8Type+`]`, `[{"k":"return","values":[{"k":"binary","op":"+","x":`+conv(u8Type, intLit("255"))+`,"y":`+conv(u8Type, intLit("1"))+`}]}]`)),
			"does not fit uint8",
		},
		{
			"typed shift overflow",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[`+u8Type+`]`, `[{"k":"return","values":[{"k":"binary","op":"<<","x":`+conv(u8Type, intLit("1"))+`,"y":`+intLit("8")+`}]}]`)),
			"does not fit uint8",
		},
		{
			"typed unsigned negation",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[`+u8Type+`]`, `[{"k":"return","values":[{"k":"unary","op":"-","x":`+conv(u8Type, intLit("1"))+`}]}]`)),
			"does not fit uint8",
		},
		{
			"typed signed division overflow",
			program(`[]`, `[]`, `[]`, fn(`[]`, `[`+i32Type+`]`, `[{"k":"return","values":[{"k":"binary","op":"/","x":`+conv(i32Type, `{"k":"unary","op":"-","x":`+intLit("2147483648")+`}`)+`,"y":`+conv(i32Type, `{"k":"unary","op":"-","x":`+intLit("1")+`}`)+`}]}]`)),
			"does not fit int32",
		},
		{
			"address outside call",
			program(`[]`, `[]`, `[{"name":"S","fields":[]}]`, fn(`[]`, `[]`, `[{"k":"var_decl","name":"s","type":{"k":"struct_ref","name":"S"},"value":null},{"k":"define","names":["p"],"value":{"k":"unary","op":"&","x":{"k":"ident","name":"s"}}}]`)),
			"direct call arguments",
		},
		{
			"unused builtin value",
			program(`[]`, `[]`, `[]`, fn(`[{"name":"s","type":{"k":"slice","elem":`+intType+`}}]`, `[]`, `[{"k":"expr_stmt","value":{"k":"builtin","fn":"len","spread":false,"type":null,"args":[{"k":"ident","name":"s"}]}}]`)),
			"only copy",
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

func TestAcceptsRangeCountAndBlankAssignment(t *testing.T) {
	for name, stmt := range map[string]string{
		"int range count":  `{"k": "range", "idx": null, "val": null, "over": {"k": "ident", "name": "n"}, "body": []}`,
		"blank assignment": `{"k": "assign", "lhs": [{"k": "ident", "name": "_"}], "value": {"k": "ident", "name": "n"}}`,
	} {
		src := `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [],
			"funcs": [{"k": "func", "name": "f",
			  "params": [{"name": "n", "type": {"k": "named", "name": "int"}}], "results": [],
			  "body": [` + stmt + `]}]}`
		if _, err := load(src); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	const twoResultDiscard = `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
		{"k": "func", "name": "pair", "params": [],
		 "results": [{"k": "named", "name": "int"}, {"k": "named", "name": "int"}],
		 "body": [{"k": "return", "values": [{"k": "int", "value": "1"}, {"k": "int", "value": "2"}]}]},
		{"k": "func", "name": "f", "params": [], "results": [],
		 "body": [{"k": "define", "names": ["_", "n"], "value": {"k": "call", "fn": "pair", "args": []}}]}
	]}`
	if _, err := load(twoResultDiscard); err != nil {
		t.Errorf("two-result blank discard: %v", err)
	}
	const oneRangeVariable = `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
		{"name": "f", "params": [{"name": "s", "type": {"k": "slice", "elem": {"k": "named", "name": "int"}}}],
		 "results": [], "body": [
			{"k": "range", "idx": "_", "val": "v", "over": {"k": "ident", "name": "s"}, "body": []},
			{"k": "range", "idx": "i", "val": "_", "over": {"k": "ident", "name": "s"}, "body": []}
		]}
	]}`
	if _, err := load(oneRangeVariable); err != nil {
		t.Errorf("one nonblank range variable: %v", err)
	}
}

func TestAcceptsTypedConstantBoundaries(t *testing.T) {
	const src = `{"vego":1,"package":"t","vars":[],"types":[],"funcs":[],"consts":[
		{"name":"add","type":null,"value":{"k":"binary","op":"+","x":{"k":"conv","type":{"k":"named","name":"uint8"},"x":{"k":"int","value":"254"}},"y":{"k":"conv","type":{"k":"named","name":"uint8"},"x":{"k":"int","value":"1"}}}},
		{"name":"neg","type":null,"value":{"k":"unary","op":"-","x":{"k":"conv","type":{"k":"named","name":"int32"},"x":{"k":"int","value":"2147483647"}}}},
		{"name":"shift","type":null,"value":{"k":"binary","op":"<<","x":{"k":"conv","type":{"k":"named","name":"uint8"},"x":{"k":"int","value":"1"}},"y":{"k":"int","value":"7"}}},
		{"name":"complement","type":null,"value":{"k":"unary","op":"^","x":{"k":"conv","type":{"k":"named","name":"uint8"},"x":{"k":"int","value":"0"}}}}
	]}`
	if _, err := load(src); err != nil {
		t.Fatal(err)
	}
}

func TestCharacterValuesAreUnicodeScalars(t *testing.T) {
	program := func(value string) string {
		return `{"vego": 1, "package": "t", "consts": [], "vars": [], "types": [], "funcs": [
			{"k": "func", "name": "f", "params": [], "results": [{"k": "named", "name": "int32"}],
			 "body": [{"k": "return", "values": [{"k": "char", "value": "` + value + `"}]}]}]}`
	}
	// 4294967296 would turn into 0 if it were cut down to 32 bits.
	for _, value := range []string{"-1", "55296", "57343", "1114112", "4294967296"} {
		if _, err := load(program(value)); err == nil || !strings.Contains(err.Error(), "invalid character value") {
			t.Errorf("character value %s: error = %v, want invalid character value", value, err)
		}
	}
	for _, value := range []string{"0", "55295", "57344", "1114111"} {
		if _, err := load(program(value)); err != nil {
			t.Errorf("character value %s: %v", value, err)
		}
	}
}

func TestArrayLengthsAreResolved(t *testing.T) {
	const prefix = `{"vego": 1, "package": "t", "consts": CONSTS, "vars": [], "types": [], "funcs": [
		{"k": "func", "name": "f", "params": [{"name": "a", "type":
		 {"k": "array", "len": LENGTH, "elem": {"k": "named", "name": "int"}}}],
		 "results": [], "body": []}]}`
	makeProgram := func(consts, length string) string {
		return strings.ReplaceAll(strings.ReplaceAll(prefix, "CONSTS", consts), "LENGTH", length)
	}
	for _, tc := range []struct {
		name   string
		length string
	}{
		{"unknown identifier", `{"k": "ident", "name": "missing"}`},
		{"negative", `{"k": "unary", "op": "-", "x": {"k": "int", "value": "1"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := load(makeProgram("[]", tc.length)); err == nil || !strings.Contains(err.Error(), "array length") {
				t.Fatalf("load error = %v, want an array length error", err)
			}
		})
	}

	constants := `[{"k": "const", "name": "n", "type": null, "value":
		{"k": "binary", "op": "+", "x": {"k": "int", "value": "1"}, "y": {"k": "int", "value": "1"}}}]`
	for _, tc := range []struct {
		name   string
		consts string
		length string
		want   int64
	}{
		{"zero", "[]", `{"k": "int", "value": "0"}`, 0},
		{"named computed constant", constants, `{"k": "ident", "name": "n"}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := load(makeProgram(tc.consts, tc.length))
			if err != nil {
				t.Fatal(err)
			}
			typ := p.FuncMap["f"].Params[0].Type
			if !typ.ALenSet || typ.ALenVal != tc.want {
				t.Fatalf("resolved length = (%d, %v), want (%d, true)", typ.ALenVal, typ.ALenSet, tc.want)
			}
		})
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
