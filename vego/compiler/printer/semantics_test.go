package printer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oneregex/revera/vego/compiler"
	"github.com/oneregex/revera/vego/compiler/export"
	"github.com/oneregex/revera/vego/compiler/printer/c"
	"github.com/oneregex/revera/vego/compiler/printer/cpp"
	"github.com/oneregex/revera/vego/compiler/printer/rust"
	"github.com/oneregex/revera/vego/compiler/printer/ts"
	"github.com/oneregex/revera/vego/compiler/printer/zig"
)

// Array lengths must preserve Go's conditional evaluation of their operands.
func TestArrayLenEvaluation(t *testing.T) {
	const source = `package sample
type State struct { N int }
func bump(s *State) [2]int {
	s.N++
	return [2]int{1, 2}
}
func Effect() int {
	var s State
	n := len(bump(&s))
	return s.N*10 + n
}
func Constant(a [][2]int) int {
	return len(a[0])
}
func NestedConstant(a [][2]int) int {
	return len([2]int{len(a[0]), len("é")})
}
func BuiltinEffect() int {
	var dst [1]int
	src := [1]int{7}
	n := len([1]int{copy(dst[:], src[:])})
	return n*10 + dst[0]
}
func ConstantConversion(a [1]uint8) int {
	n := len([1]string{string(a[:])})
	return n + int(a[0])
}
`
	writeFile := func(t *testing.T, dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srcDir := t.TempDir()
	writeFile(t, srcDir, "sample.go", source)
	blob, violations, err := export.Package(srcDir)
	if err != nil || len(violations) != 0 {
		t.Fatalf("export: %v, %v", err, violations)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "c", "cpp", "rust", "zig", "ts"} {
		t.Run(target, func(t *testing.T) {
			write := func(dir, name, content string) {
				t.Helper()
				writeFile(t, dir, name, content)
			}
			tool := map[string]string{"go": "go", "c": "cc", "cpp": "c++", "rust": "rustc", "zig": "zig", "ts": "node"}[target]
			if _, err := exec.LookPath(tool); err != nil {
				t.Skipf("%s is not installed", tool)
			}
			p, err := compiler.Load(blob)
			if err != nil {
				t.Fatal(err)
			}
			if err := compiler.Check(p); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			copyRuntime := func(rel, name string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(repo, rel))
				if err != nil {
					t.Fatal(err)
				}
				write(dir, name, string(data))
			}
			var commands [][]string
			switch target {
			case "go":
				write(dir, "sample.go", strings.Replace(source, "package sample", "package main", 1))
				write(dir, "main.go", "package main\nfunc main() { if Effect() != 12 || BuiltinEffect() != 17 { panic(\"effect\") }; if Constant(nil) != 2 || NestedConstant(nil) != 2 || ConstantConversion([1]uint8{7}) != 8 { panic(\"constant\") } }\n")
				commands = [][]string{{"go", "run", "sample.go", "main.go"}}
			case "c":
				header, body, err := c.Emit(p, c.Options{HeaderName: "engine.h"})
				if err != nil {
					t.Fatal(err)
				}
				write(dir, "engine.h", header)
				write(dir, "engine.c", body)
				copyRuntime("native/c/vg.h", "vg.h")
				write(dir, "main.c", "#include \"engine.h\"\nint main(void) { if (sample_Effect() != 12 || sample_BuiltinEffect() != 17) return 1; return sample_Constant((sample_slice_arr_i64_2){0}) != 2 || sample_NestedConstant((sample_slice_arr_i64_2){0}) != 2 || sample_ConstantConversion((sample_arr_u8_1){{7}}) != 8; }\n")
				commands = [][]string{{"cc", "-std=c11", "-fwrapv", "engine.c", "main.c", "-o", "run"}, {"./run"}}
			case "cpp":
				header, body, err := cpp.Emit(p, cpp.Options{HeaderName: "engine.hpp", Namespace: "sample"})
				if err != nil {
					t.Fatal(err)
				}
				write(dir, "engine.hpp", header)
				write(dir, "engine.cpp", body)
				copyRuntime("native/cpp/vg.hpp", "vg.hpp")
				write(dir, "main.cpp", "#include \"engine.hpp\"\nint main() { if (sample::Effect() != 12 || sample::BuiltinEffect() != 17) return 1; return sample::Constant({}) != 2 || sample::NestedConstant({}) != 2 || sample::ConstantConversion({7}) != 8; }\n")
				commands = [][]string{{"c++", "-std=c++20", "-fwrapv", "engine.cpp", "main.cpp", "-o", "run"}, {"./run"}}
			case "rust":
				body, err := rust.Emit(p)
				if err != nil {
					t.Fatal(err)
				}
				write(dir, "engine.rs", body)
				copyRuntime("rust/src/vg.rs", "vg.rs")
				write(dir, "main.rs", "mod vg; mod engine; fn main() { assert_eq!(engine::Effect(), 12); assert_eq!(engine::BuiltinEffect(), 17); assert_eq!(engine::Constant(vg::zero()), 2); assert_eq!(engine::NestedConstant(vg::zero()), 2); assert_eq!(engine::ConstantConversion([7]), 8); }\n")
				commands = [][]string{{"rustc", "--edition=2021", "main.rs", "-o", "run"}, {"./run"}}
			case "zig":
				body, err := zig.Emit(p)
				if err != nil {
					t.Fatal(err)
				}
				write(dir, "engine.zig", body)
				copyRuntime("zig/src/vg.zig", "vg.zig")
				write(dir, "main.zig", "const std = @import(\"std\"); const engine = @import(\"engine.zig\"); pub fn main() void { std.debug.assert(engine.Effect() == 12); std.debug.assert(engine.BuiltinEffect() == 17); std.debug.assert(engine.Constant(.{}) == 2); std.debug.assert(engine.NestedConstant(.{}) == 2); std.debug.assert(engine.ConstantConversion(.{7}) == 8); }\n")
				commands = [][]string{{"zig", "run", "main.zig"}}
			case "ts":
				body, err := ts.Emit(p)
				if err != nil {
					t.Fatal(err)
				}
				write(dir, "engine.ts", body)
				copyRuntime("ts/src/vg.ts", "vg.ts")
				write(dir, "package.json", "{\"type\":\"module\"}\n")
				write(dir, "main.ts", "import * as e from './engine.ts'; import * as vg from './vg.ts'; if (e.Effect() !== 12 || e.BuiltinEffect() !== 17) throw new Error('effect'); if (e.Constant(vg.NIL) !== 2 || e.NestedConstant(vg.NIL) !== 2 || e.ConstantConversion(new Uint8Array([7])) !== 8) throw new Error('constant');\n")
				commands = [][]string{{"node", "main.ts"}}
			}
			for _, args := range commands {
				cmd := exec.Command(args[0], args[1:]...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%v: %v\n%s", args, err, out)
				}
			}
		})
	}
}
