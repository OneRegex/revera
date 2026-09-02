package rust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
)

func TestSyntheticTempsAvoidProgramNames(t *testing.T) {
	f := &compiler.FuncDecl{Info: map[string]*compiler.LocalInfo{"_t1": {}}}
	g := &gen{p: &compiler.Program{Funcs: []*compiler.FuncDecl{f}}}
	g.resetNames(f)
	if got := g.newTmp(); got != "_t2" {
		t.Fatalf("first synthetic temporary = %q, want %q", got, "_t2")
	}
}

func TestReservedRustKeywordsUseRawIdentifiers(t *testing.T) {
	for _, name := range []string{"abstract", "become"} {
		if got, want := ident(name), "r#"+name; got != want {
			t.Fatalf("ident(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestStructEqualityHelperAvoidsProgramName(t *testing.T) {
	const src = `{"vego":1,"package":"collision","consts":[],"vars":[],"types":[
		{"k":"type","name":"S","fields":[{"name":"X","type":{"k":"named","name":"int"}}]}
	],"funcs":[
		{"k":"func","name":"vego_eq_S","params":[
			{"name":"left","type":{"k":"struct_ref","name":"S"}},
			{"name":"right","type":{"k":"struct_ref","name":"S"}}],
		 "results":[{"k":"named","name":"bool"}],
		 "body":[{"k":"return","values":[{"k":"bool","value":true}]}]},
		{"k":"func","name":"equal","params":[
			{"name":"left","type":{"k":"struct_ref","name":"S"}},
			{"name":"right","type":{"k":"struct_ref","name":"S"}}],
		 "results":[{"k":"named","name":"bool"}],
		 "body":[{"k":"return","values":[{"k":"binary","op":"==",
			"x":{"k":"ident","name":"left"},"y":{"k":"ident","name":"right"}}]}]}
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
		"pub fn vego_eq_S(a: S, b: S)",
		"pub fn vego_vego_eq_S(left: S, right: S)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated output does not contain %q:\n%s", want, out)
		}
	}

	rustc, err := exec.LookPath("rustc")
	if err != nil {
		t.Skip("rustc is not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "engine.rs"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib.rs"), []byte("mod vg {}\nmod engine;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(rustc, "--edition=2021", "--crate-type=lib", "lib.rs")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rustc failed: %v\n%s", err, output)
	}
}
