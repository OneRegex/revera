// Command vegoc checks, exports and prints Vego programs.
//
// Usage:
//
//	vegoc check <package-directory>
//	vegoc export [-o output.json] <package-directory>
//	vegoc emit go   [-o engine.go] input.json
//	vegoc emit rust [-o engine.rs] input.json
//	vegoc emit zig  [-o engine.zig] input.json
//	vegoc emit ts   [-o engine.ts] input.json
//	vegoc emit cpp  [-header engine.hpp] [-source engine.cpp] [-namespace name] input.json
//	vegoc emit c    [-header engine.h] [-source engine.c] [-prefix name] input.json
//	vegoc version
//
// check reports every construct outside the subset with a file and a line.
// export does the same check and then writes the JSON form of the package.
// emit reads the JSON form and prints it in one target language.
// An output flag left empty sends the text to standard output.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/oneregex/revera/vego/compiler"
	"github.com/oneregex/revera/vego/compiler/export"
	"github.com/oneregex/revera/vego/compiler/printer/c"
	"github.com/oneregex/revera/vego/compiler/printer/cpp"
	"github.com/oneregex/revera/vego/compiler/printer/golang"
	"github.com/oneregex/revera/vego/compiler/printer/rust"
	"github.com/oneregex/revera/vego/compiler/printer/ts"
	"github.com/oneregex/revera/vego/compiler/printer/zig"
)

const usageText = `usage:
  vegoc check <package-directory>
  vegoc export [-o output.json] <package-directory>
  vegoc emit go   [-o engine.go] input.json
  vegoc emit rust [-o engine.rs] input.json
  vegoc emit zig  [-o engine.zig] input.json
  vegoc emit ts   [-o engine.ts] input.json
  vegoc emit cpp  [-header engine.hpp] [-source engine.cpp] [-namespace name] input.json
  vegoc emit c    [-header engine.h] [-source engine.c] [-prefix name] input.json
  vegoc version

An output flag left empty sends the text to standard output.`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stderr)
	case "export":
		return runExport(args[1:], stdout, stderr)
	case "emit":
		return runEmit(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "vegoc %s\n", compiler.Version)
		return 0
	case "-h", "-help", "--help", "help":
		fmt.Fprintln(stdout, usageText)
		return 0
	}
	fmt.Fprintf(stderr, "unknown command %q\n\n%s\n", args[0], usageText)
	return 2
}

func newFlagSet(name string, stderr io.Writer, usage string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprintln(stderr, usage) }
	return flags
}

// parse runs the flag set and reports whether the command can go on.
// The exit status is meaningful only when the boolean is false.
func parse(flags *flag.FlagSet, args []string, positional int, stderr io.Writer) (bool, int) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}
		return false, 2
	}
	if flags.NArg() != positional {
		flags.Usage()
		return false, 2
	}
	return true, 0
}

// exportPackage runs the front end and prints the violations, if any.
func exportPackage(dir string, stderr io.Writer) ([]byte, bool) {
	blob, violations, err := export.Package(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, false
	}
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Fprintln(stderr, v)
		}
		fmt.Fprintf(stderr, "%d subset violation(s)\n", len(violations))
		return nil, false
	}
	return blob, true
}

func runCheck(args []string, stderr io.Writer) int {
	flags := newFlagSet("check", stderr, "usage: vegoc check <package-directory>")
	if ok, status := parse(flags, args, 1, stderr); !ok {
		return status
	}
	if _, ok := exportPackage(flags.Arg(0), stderr); !ok {
		return 1
	}
	return 0
}

func runExport(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("export", stderr, "usage: vegoc export [-o output.json] <package-directory>")
	out := flags.String("o", "", "output file (default stdout)")
	if ok, status := parse(flags, args, 1, stderr); !ok {
		return status
	}
	blob, ok := exportPackage(flags.Arg(0), stderr)
	if !ok {
		return 1
	}
	return write(*out, blob, stdout, stderr)
}

func runEmit(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usageText)
		return 2
	}
	target, rest := args[0], args[1:]
	switch target {
	case "go":
		return emitOne(target, rest, stdout, stderr, func(blob []byte) (string, error) {
			// The Go printer works on the raw JSON, so the front end runs first only to reject an ill-typed program.
			if _, err := load(blob); err != nil {
				return "", err
			}
			return golang.Emit(blob)
		})
	case "rust":
		return emitOne(target, rest, stdout, stderr, func(blob []byte) (string, error) {
			p, err := load(blob)
			if err != nil {
				return "", err
			}
			return rust.Emit(p)
		})
	case "zig":
		return emitOne(target, rest, stdout, stderr, func(blob []byte) (string, error) {
			p, err := load(blob)
			if err != nil {
				return "", err
			}
			return zig.Emit(p)
		})
	case "ts":
		return emitOne(target, rest, stdout, stderr, func(blob []byte) (string, error) {
			p, err := load(blob)
			if err != nil {
				return "", err
			}
			return ts.Emit(p)
		})
	case "cpp":
		flags := newFlagSet("emit cpp", stderr,
			"usage: vegoc emit cpp [-header engine.hpp] [-source engine.cpp] [-namespace name] input.json")
		header := flags.String("header", "", "output header (default stdout)")
		source := flags.String("source", "", "output source (default stdout)")
		namespace := flags.String("namespace", "", "output namespace (default the package name)")
		if ok, status := parse(flags, rest, 1, stderr); !ok {
			return status
		}
		return emitPair(flags.Arg(0), *header, *source, stdout, stderr, func(p *compiler.Program) (string, string, error) {
			return cpp.Emit(p, cpp.Options{HeaderName: filepath.Base(*header), Namespace: *namespace})
		})
	case "c":
		flags := newFlagSet("emit c", stderr,
			"usage: vegoc emit c [-header engine.h] [-source engine.c] [-prefix name] input.json")
		header := flags.String("header", "", "output header (default stdout)")
		source := flags.String("source", "", "output source (default stdout)")
		prefix := flags.String("prefix", "", "global identifier prefix (default the package name)")
		if ok, status := parse(flags, rest, 1, stderr); !ok {
			return status
		}
		return emitPair(flags.Arg(0), *header, *source, stdout, stderr, func(p *compiler.Program) (string, string, error) {
			return c.Emit(p, c.Options{HeaderName: filepath.Base(*header), Prefix: *prefix})
		})
	}
	fmt.Fprintf(stderr, "unknown emit target %q; the targets are go, rust, zig, ts, cpp and c\n", target)
	return 2
}

// load runs the shared front end of the printers over the JSON form.
func load(blob []byte) (*compiler.Program, error) {
	p, err := compiler.Load(blob)
	if err != nil {
		return nil, err
	}
	if err := compiler.Check(p); err != nil {
		return nil, err
	}
	return p, nil
}

func emitOne(target string, args []string, stdout, stderr io.Writer, emit func([]byte) (string, error)) int {
	flags := newFlagSet("emit "+target, stderr, fmt.Sprintf("usage: vegoc emit %s [-o output] input.json", target))
	out := flags.String("o", "", "output file (default stdout)")
	if ok, status := parse(flags, args, 1, stderr); !ok {
		return status
	}
	blob, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	text, err := emit(blob)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return write(*out, []byte(text), stdout, stderr)
}

func emitPair(input, header, source string, stdout, stderr io.Writer,
	emit func(*compiler.Program) (string, string, error)) int {
	p, err := compiler.LoadFile(input)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	headerText, sourceText, err := emit(p)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if status := write(header, []byte(headerText), stdout, stderr); status != 0 {
		return status
	}
	return write(source, []byte(sourceText), stdout, stderr)
}

// write sends data to the named file, or to stdout when the name is empty.
func write(path string, data []byte, stdout, stderr io.Writer) int {
	if path == "" {
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
