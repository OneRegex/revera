package export

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestSchemaNamesEveryNodeKind checks that the structural schema of the IR is valid JSON and names every node kind the exporter can emit.
// A full validation needs a JSON Schema library, which this module does not depend on; the printers and the Lean decoder are the real consumers.
func TestSchemaNamesEveryNodeKind(t *testing.T) {
	raw, err := os.ReadFile("../../schema/vego.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("the schema is not valid JSON: %v", err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("unexpected $schema value %v", schema["$schema"])
	}
	text := string(raw)
	kinds := []string{"named", "slice", "array", "struct_ref", "ptr",
		"const", "var", "type", "func",
		"var_decl", "define", "assign", "op_assign", "incdec", "if", "for", "range", "switch", "break", "continue", "return", "expr_stmt", "block",
		"int", "char", "str", "bool", "ident", "field", "index", "slice_expr", "call", "builtin", "conv", "unary", "binary", "composite"}
	for _, kind := range kinds {
		if !strings.Contains(text, `"`+kind+`"`) {
			t.Errorf("the schema does not name the node kind %q", kind)
		}
	}
}
