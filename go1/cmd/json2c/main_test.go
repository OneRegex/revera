package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"revera1/vegoc"
)

func TestReservedIdentifierMappingIsInjective(t *testing.T) {
	got := []string{ident("int"), ident("int_"), ident("vego_int")}
	want := []string{"vego_int", "int_", "vego_vego_int"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mapped identifiers = %q, want %q", got, want)
		}
	}
}

func TestRuntimeAndContextNamesAreEscaped(t *testing.T) {
	for _, name := range []string{"mem", "restrict", "errno", "vg_str", "bool"} {
		if got, want := ident(name), "vego_"+name; got != want {
			t.Fatalf("ident(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSyntheticTempsAvoidProgramNames(t *testing.T) {
	f := &vegoc.FuncDecl{Info: map[string]*vegoc.LocalInfo{"_t1": {}}}
	g := &gen{p: &vegoc.Program{Funcs: []*vegoc.FuncDecl{f}}}
	g.resetNames(f)
	if got := g.newTmp(); got != "_t2" {
		t.Fatalf("first synthetic temporary = %q, want %q", got, "_t2")
	}
}

func TestLogicalRightOperandStaysConditional(t *testing.T) {
	// The right operand allocates, so its prelude must run only when the left operand is true.
	sliceInt := &vegoc.Type{K: vegoc.KSlice, Elem: vegoc.TInt}
	f := &vegoc.FuncDecl{Info: map[string]*vegoc.LocalInfo{}}
	g := &gen{p: &vegoc.Program{}, fn: f}
	g.resetNames(f)
	g.depth = 1
	e := &vegoc.Expr{K: "binary", Op: "&&", Typ: vegoc.TBool,
		X: &vegoc.Expr{K: "ident", Name: "ok", Typ: vegoc.TBool},
		Y: &vegoc.Expr{K: "binary", Op: ">", Typ: vegoc.TBool,
			X: &vegoc.Expr{K: "builtin", Name: "len", Typ: vegoc.TInt,
				Args: []*vegoc.Expr{{K: "builtin", Name: "make", Typ: sliceInt, TypeRef: sliceInt,
					Args: []*vegoc.Expr{{K: "call", Name: "n", Typ: vegoc.TInt}}}}},
			Y: &vegoc.Expr{K: "call", Name: "n", Typ: vegoc.TInt}}}
	got := g.expr(e)
	if got != "_t1" {
		t.Fatalf("logical expression = %q, want a temporary", got)
	}
	joined := strings.Join(g.pre, "\n")
	if !strings.Contains(joined, "bool _t1 = ok;") || !strings.Contains(joined, "if (_t1) {") {
		t.Fatalf("prelude does not guard the right operand:\n%s", joined)
	}
}

// TestGeneratedSourcesCompile runs the printer over both real Vego programs and compiles the output.
// It uses the same flags as the c1 Makefile, with warnings as errors.
func TestGeneratedSourcesCompile(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is not installed")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	vgh, err := os.ReadFile(filepath.Join(repo, "..", "c1", "vg.h"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		json   string
		prefix string
	}{
		{json: "revera.vego.json", prefix: "revera_eng"},
		{json: "probe.vego.json", prefix: ""},
	}
	for _, tc := range cases {
		t.Run(tc.json, func(t *testing.T) {
			p, err := vegoc.LoadFile(filepath.Join(repo, tc.json))
			if err != nil {
				t.Fatal(err)
			}
			g := &gen{p: p, hdrName: "engine.h", prefix: p.Package}
			if tc.prefix != "" {
				g.prefix = tc.prefix
			}
			header, source := g.files()
			dir := t.TempDir()
			for name, content := range map[string]string{
				"engine.h": header, "engine.c": source, "vg.h": string(vgh),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(cc, "-std=c11", "-fwrapv", "-Wall", "-Wextra",
				"-Wno-parentheses-equality", "-Werror", "-c", "engine.c", "-o", os.DevNull)
			cmd.Dir = dir
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("C compile failed: %v\n%s", err, output)
			}
		})
	}
}
