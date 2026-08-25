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

func TestCheckEngine(t *testing.T) {
	p := loadEngine(t)
	if len(p.Funcs) == 0 || len(p.Types) == 0 {
		t.Fatal("empty program")
	}
	// Every expression that a printer needs a type for must have
	// one. Walk everything and verify.
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
