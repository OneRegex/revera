package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"revera1/codesize"
	"revera1/conformance"
)

// sizeRow is the code-size report of one engine.
type sizeRow struct {
	name        string
	sourceBytes int64
	sourceLines int64
	codeBytes   uint64
	functions   int
	textBytes   uint64
}

func runSize(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("size", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprintln(stderr, usageText) }
	repoValue, backendValues := commonFlags(flags)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, usageText)
		return 2
	}
	repo, backends, err := resolve(*repoValue, *backendValues)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rows := []sizeRow{}
	goRow, err := goSize(repo)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rows = append(rows, goRow)
	for _, b := range backends {
		row, err := backendSize(b)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		rows = append(rows, row)
	}
	jsonInfo, err := os.Stat(filepath.Join(repo, "go1", "revera.vego.json"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "generated code size; revera.vego.json is %d bytes\n", jsonInfo.Size())
	fmt.Fprintln(stdout, "source is the engine source of the language, code is the machine code of the engine functions in the release driver, text is the whole executable section of that driver")
	table := [][]string{{"engine", "source bytes", "source lines", "code bytes", "functions", "driver text bytes"}}
	for _, r := range rows {
		table = append(table, []string{r.name, fmt.Sprint(r.sourceBytes), fmt.Sprint(r.sourceLines),
			fmt.Sprint(r.codeBytes), fmt.Sprint(r.functions), fmt.Sprint(r.textBytes)})
	}
	conformance.WriteTable(stdout, table, true)
	return 0
}

// goSize builds the Go driver and sizes the Vego package in it, without its host files.
func goSize(repo string) (sizeRow, error) {
	go1 := filepath.Join(repo, "go1")
	out := filepath.Join(repo, "tmp", "bench", "godriver")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return sizeRow{}, err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", out, "./cmd/godriver")
	cmd.Dir = go1
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return sizeRow{}, fmt.Errorf("build the Go driver: %w", err)
	}
	drop, err := hostFunctionPattern(filepath.Join(go1, "revera"))
	if err != nil {
		return sizeRow{}, err
	}
	report, err := codesize.Analyze(out)
	if err != nil {
		return sizeRow{}, err
	}
	row := sizeRow{name: "go1", textBytes: report.TextBytes}
	row.codeBytes, row.functions = report.Sum(regexp.MustCompile(`^revera1/revera\.`), drop)
	sources, err := filepath.Glob(filepath.Join(go1, "revera", "*.go"))
	if err != nil {
		return sizeRow{}, err
	}
	for _, path := range sources {
		if strings.HasSuffix(path, "_host.go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		if err := addSource(&row, path); err != nil {
			return sizeRow{}, err
		}
	}
	return row, nil
}

// hostFunctionPattern matches the symbols of the functions that the host files declare.
// Those functions are Go-only, so the engine figure leaves them out.
func hostFunctionPattern(dir string) (*regexp.Regexp, error) {
	hosts, err := filepath.Glob(filepath.Join(dir, "*_host.go"))
	if err != nil {
		return nil, err
	}
	names := []string{"init"}
	fset := token.NewFileSet()
	for _, path := range hosts {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				switch t := fn.Recv.List[0].Type.(type) {
				case *ast.StarExpr:
					name = "(*" + t.X.(*ast.Ident).Name + ")." + name
				case *ast.Ident:
					name = t.Name + "." + name
				}
			}
			names = append(names, regexp.QuoteMeta(name))
		}
	}
	return regexp.MustCompile(`^revera1/revera\.(` + strings.Join(names, "|") + `)(\.|$)`), nil
}

func backendSize(b *conformance.Backend) (sizeRow, error) {
	row := sizeRow{name: b.Name}
	for _, rel := range b.Generated {
		if err := addSource(&row, b.Path(rel)); err != nil {
			return sizeRow{}, err
		}
	}
	driver := b.Path(b.Release.Driver)
	if _, err := os.Stat(driver); err != nil {
		return sizeRow{}, fmt.Errorf("%s: release driver missing; build it first: %v", b.Name, err)
	}
	report, err := codesize.Analyze(driver)
	if err != nil {
		return sizeRow{}, err
	}
	row.textBytes = report.TextBytes
	if b.Symbols == nil {
		return sizeRow{}, fmt.Errorf("%s: backend.json has no engine_symbols expression", b.Name)
	}
	row.codeBytes, row.functions = report.Sum(b.Symbols, nil)
	if row.functions == 0 {
		return sizeRow{}, fmt.Errorf("%s: engine_symbols %q matches no function in %s", b.Name, b.EngineSymbols, b.Release.Driver)
	}
	return row, nil
}

func addSource(row *sizeRow, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	row.sourceBytes += int64(len(data))
	row.sourceLines += int64(bytes.Count(data, []byte{'\n'}))
	return nil
}
