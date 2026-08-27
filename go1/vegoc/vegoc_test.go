package vegoc

import (
	"os"
	"testing"
)

func loadEngine(t *testing.T) *Program {
	t.Helper()
	blob, err := os.ReadFile("../revera.vego.json")
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
