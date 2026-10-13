package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

func TestCharacterLiterals(t *testing.T) {
	const input = `{"vego":1,"package":"p","consts":[],
		"vars":[{"k":"var","name":"global","type":null,"value":{"k":"char","value":"66"}}],
		"types":[],"funcs":[{"k":"func","name":"local","params":[],
		"results":[{"k":"named","name":"int32"}],"body":[
			{"k":"define","names":["x"],"value":{"k":"char","value":"VALUE"}},
			{"k":"return","values":[{"k":"ident","name":"x"}]}]}]}`
	// The literal must keep its rune type, or returning x as an int32 fails to compile.
	src, err := Emit([]byte(strings.ReplaceAll(input, "VALUE", "65")))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "generated.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := new(types.Config).Check("p", fset, []*ast.File{file}, nil); err != nil {
		t.Fatalf("type-check generated Go: %v\n%s", err, src)
	}
	// A surrogate is not a character, and quoting it would silently turn it into U+FFFD.
	if _, err := Emit([]byte(strings.ReplaceAll(input, "VALUE", "55296"))); err == nil || !strings.Contains(err.Error(), "invalid character value") {
		t.Fatalf("surrogate character value: error = %v, want invalid character value", err)
	}
}

func TestRenderValidatesDocumentHeader(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"missing fields", `{}`, "version"},
		{"unsupported version", `{"vego": 2, "package": "p", "consts": [], "vars": [], "types": [], "funcs": []}`, "unsupported"},
		{"malformed list", `{"vego": 1, "package": "p", "consts": {}, "vars": [], "types": [], "funcs": []}`, "consts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Emit([]byte(tc.src)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("render error = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestRenderKeepsAddressAsDirectCallArgument(t *testing.T) {
	const input = `{"vego":1,"package":"p","consts":[],"vars":[],
		"types":[{"k":"type","name":"S","fields":[]}],
		"funcs":[
			{"k":"func","name":"take","params":[{"name":"s","type":{"k":"ptr","name":"S"}}],"results":[],"body":[]},
			{"k":"func","name":"call","params":[],"results":[],"body":[
				{"k":"var_decl","name":"s","type":{"k":"struct_ref","name":"S"},"value":null},
				{"k":"expr_stmt","value":{"k":"call","fn":"take","args":[
					{"k":"unary","op":"&","x":{"k":"ident","name":"s"}}]}}]}]}`
	src, err := Emit([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "generated.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if ok && name.Name == "take" {
			_, found = call.Args[0].(*ast.UnaryExpr)
		}
		return true
	})
	if !found {
		t.Fatalf("address argument was not direct:\n%s", src)
	}
}

func TestRenderMinimalDocument(t *testing.T) {
	src, err := Emit([]byte(`{"vego": 1, "package": "p", "consts": [], "vars": [], "types": [], "funcs": []}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "package p") {
		t.Fatalf("rendered source does not contain the package declaration: %q", src)
	}
}
