# go1: the revera engine in Vego

This directory rewrites the go0 ERE engine in Vego, a strict Go subset built for mechanical translation into other languages.

[`VEGO-SPECIFICATION.md`](VEGO-SPECIFICATION.md) defines the subset and its JSON representation in full.

## Layout

- `revera/` is the engine, in one flat package.
  Every file is in the subset except `revera_host.go`, `corpus_host.go`, `driver_host.go`, and the tests.
  The former locale package is merged in.
  The CLDR data blob travels as a string parameter, and the host file embeds `data.bin` for Go builds.
- `cmd/vego2json` checks subset conformance and translates the package into one JSON object.
- `cmd/revera` regenerates or checks all JSON and target source artifacts in one transaction.
- `revera.vego.json` is that JSON form of the engine.
- `cmd/json2go` is the reference converter, which turns the JSON back into a compilable Go file.
  The same recipe with a different printer produces the C++, Rust or Zig instantiation.
- `vegoc` is the shared front end of the target printers.
  It loads the JSON, types every expression, folds constants, and computes local usage and mutation.
- `cmd/json2zig`, `cmd/json2cpp` and `cmd/json2rust` print the engine in the target languages.
  The results and their runtimes live in `../zig1`, `../cpp1` and `../rust1`.
- `cmd/crosscheck` verifies the target drivers against the Go engine.
  `cmd/godriver` runs the same driver protocol with the Go engine, which helps when a driver needs debugging.
- `probe/` is a second Vego package, exported as `probe.vego.json`.
  It covers the subset constructs the engine never uses: range statements, division overflow, byte-slice conversion, comparable structs, evaluation order, and spare-capacity zeroing.
  `cmd/proberef` prints its results with the Go original, and `cmd/probecheck` diffs each target against them.

## Using it

```go
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
  A subject that does not match gives `nil`.
- `Error` implements the `error` interface.
- `OpenLocale("cs", "")` selects a CLDR locale for bracket expressions.
  The default is POSIX.
- `re.Contract(maxInput)` reports what one match can cost, before it runs.

## API differences against go0

The subset has no methods, no interfaces and no function values.
The low-level surface below the host file therefore changed shape, without a change in behavior:

- Functions replace methods: `Exec(&re, subject, pmatch, eflags)` instead of `re.Exec(...)`.
  `Compile` returns the `Regexp` by value.
- Errors are values: `Error{Code, Pos}`, where `ErrNone` means success.
  They are not a Go `error`.
- Flags and error codes are plain `uint32` and `int32` constants, such as `FlagICase`, `ExecNotBOL` and `ErrESpace`.
  The numeric values match go0.
  The execution flags reach only this level, because the methods above pass none.
- The callback API became an iterator.
  `MatchIterInit` and `MatchIterNext` drive the scan, and `ReplaceAll` builds on them.
- `ContractFor(&re, maxInput)` computes the resource contract.
  Its byte constants track the records of this implementation, which are index arenas and open-addressing memo tables.
  The figures therefore differ from go0 while they cover the same allocations.
- Workspaces are fresh per `Exec` call instead of pooled.
  `Compile` never writes to a `Regexp` again, so concurrent `Exec` calls stay safe in Go.

One robustness improvement came with the rewrite.
The arenas of the capture solver are capped.
An input that drives go0 into tens of gigabytes before its work limit reports `ESpace` now reports `ESpace` early, with bounded memory.

## Verification

`revera/differential_test.go` and `revera/locale_test.go` compare this engine against go0.
They cover random and fixed pattern corpora, every flag, locales with multi-character collating elements, UTF-8 handling, every locale operation, replacement, and iteration.
A reference matcher that enumerates every parse checks go0 itself, and the host `regcomp()` checks it again.
Agreement with go0 therefore carries that assurance over.

The JSON pipeline closes the first loop:

```sh
go run ./cmd/revera generate
go run ./cmd/revera check-generated
go run ./cmd/json2go -o /tmp/engine_gen.go revera.vego.json
```

Use `-target rust`, `-target zig`, `-target cpp`, or a comma-separated selection for a partial target regeneration.
Both JSON files remain shared prerequisites and are always regenerated.
The command stages every selected output below `../tmp/` and changes checked-in files only after all producers succeed.
`check-generated` compares bytes and never repairs stale files.

The regenerated engine passes the same test suite.
The JSON therefore carries the whole program, which is what the Rust, C++, Zig and LEAN4 consumers need.

The target instantiations close the second loop.
Each target builds a driver that speaks a small line protocol, defined in `revera/driver_host.go`.
`cmd/crosscheck` feeds every driver the corpora of the differential tests and compares against the Go engine line by line:

```sh
go run ./cmd/crosscheck ../zig1/zig-out/bin/driver \
    ../cpp1/driver ../rust1/target/release/driver
```

The corpus covers random patterns over every flag set, the fixed patterns, the cs locale with its multi-character collating element, replacement, iteration, resource contracts, and locale case-map sweeps.
