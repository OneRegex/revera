# Revera for Go

This module is the Go implementation of the Revera POSIX.1-2024 extended regular expression engine.
The five language implementations are cross-checked on the shared conformance corpus.
The module path is `github.com/oneregex/revera/go`, and it has no dependency.

The engine is written in Vego, the strict Go subset that [`../vego/SPECIFICATION.md`](../vego/SPECIFICATION.md) defines.
This directory is therefore also the canonical source of every other implementation.
`vegoc export` turns it into `../revera.vego.json`, and the printers of `vegoc emit` turn that file into the Rust, Zig, C++ and C engines.

## Using it

```sh
go get github.com/oneregex/revera/go
```

```go
import "github.com/oneregex/revera/go"

re, err := revera.New("([a-z]+)([0-9]*)")
groups, err := re.FindStringSubmatch("__abc12__")
// groups == []string{"abc12", "abc", "12"}
```

`revera_host.go` holds the idiomatic Go surface.
It is a host file, outside the subset, so every target writes the same thing for itself.

- `New` compiles and returns `(*Regexp, error)`.
  `MustNew` panics instead, for patterns fixed at build time.
- The options are functions: `CaseInsensitive()`, `NewlineSensitive()`, `NoCaptures()`, `ShortestMatch()`, and `In(loc)` for a locale.
- The methods take the names of the standard `regexp` package: `MatchString`, `FindStringIndex`, `FindStringSubmatch`, `FindAllString`, `ReplaceAllString`, and the rest.
  Each one adds an `error` result, because a subject can exceed the capacity of the engine.
  Slice-returning find methods return `nil` when there is no match; `MatchString` returns false, and `FindString` distinguishes an empty match with its boolean result.
- `Error` implements the `error` interface.
- `OpenLocale("cs", "")` selects a CLDR locale for bracket expressions.
  The default is POSIX.
- `re.Contract(maxInput)` reports what one search can cost, before it runs.

## Layout

Every non-test Go file is in the Vego subset except `revera_host.go`.

- The subset files implement parsing, compilation, matching, capture recovery, replacement, resource contracts, UTF-8 decoding and locale lookup.
- `revera_host.go` is the public Go API.
  It is the only host file; the line protocols and the shared cases that every target implements live in `../dev/internal/protocol`, so this module exports nothing but the engine.
- `LICENSE` covers the engine under the MIT license, and `LICENSES/Unicode-3.0.txt` covers the embedded Unicode and CLDR data.
  `make licenses` at the repository root keeps both copies current.
- `data.bin` is the CLDR locale blob, embedded by the host file.
  Six copies of it exist in the repository, and the conformance kit checks that they stay identical.
- `testdata/locale-expected.tsv.gz` holds the locale answers of the reference engine.
  `locale_test.go` replays it, so the module tests need no dependency.

## The low-level surface

The subset has no methods, no interfaces and no function values.
Below the host file, the engine therefore has a different shape from an ordinary Go package:

- Functions replace methods: `Exec(&re, subject, pmatch, eflags)` instead of `re.Exec(...)`.
  `Compile` returns the `Regexp` by value.
- Errors are values: `Error{Code, Pos}`, where `ErrNone` means success.
  They are not a Go `error`.
- Flags and error codes are plain `uint32` and `int32` constants, such as `FlagICase`, `ExecNotBOL` and `ErrESpace`.
- The callback API of the reference engine became an iterator.
  `MatchIterInit` and `MatchIterNext` drive the scan, and `ReplaceAll` builds on them.
- `ContractFor(&re, maxInput)` computes the resource contract.
- Workspaces are fresh per `Exec` call.
  `Compile` never writes to a `Regexp` again, so concurrent `Exec` calls stay safe.

The arenas of the capture solver are capped.
An input that would need tens of gigabytes reports `ESpace` early, with bounded memory.

## Verification

```sh
GOWORK=off go test ./...
GOWORK=off go test -run '^$' -fuzz FuzzEngine -fuzztime 60s .
```

The module tests cover the public API, malformed locale data and the locale answers of the reference engine.
`FuzzEngine` is a crash-only fuzz entry point over the shared input layout, so a corpus transfers between the targets.

The comparisons with the reference engine live in the development module, so that this module stays free of dependencies:

```sh
cd ../dev
go test -count=1 -timeout 30m ./internal/differential
go test -run '^$' -fuzz FuzzDifferential -fuzztime 60s ./internal/differential
```

These differential and cross-language checks cover finite corpora.
The Lean development proves universal phase A heap and step bounds under its stated hypotheses, while its full engine-to-specification checks cover a finite corpus and exhaustive small domain; [`../lean/README.md`](../lean/README.md) gives the exact boundary.

An edit to a subset file changes the exported IR.
Run `make generate` at the repository root afterwards, then `make check-generated`, and the rest of the pipeline follows from [`../README.md`](../README.md).
