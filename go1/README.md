# go1: the revera engine in Vego

This directory rewrites the go0 ERE engine in Vego, a strict Go
subset built for mechanical translation into other languages.
[`VEGO-SPECIFICATION.md`](VEGO-SPECIFICATION.md) defines the subset
and its JSON representation in full.

## Layout

- `revera/` is the engine, one flat package. Every file is in the
  subset except `revera_host.go` and the tests. The former locale
  package is merged in; the CLDR data blob travels as a string
  parameter, and the host file embeds `data.bin` for Go builds.
- `cmd/vego2json` checks subset conformance and translates the
  package into one JSON object.
- `cmd/json2go` is the reference converter: it turns that JSON back
  into a compilable Go file. The same recipe with a different
  printer produces the C++, Rust, or Zig instantiation.
- `revera.vego.json` is the generated JSON form of the engine.
- `vegoc` is the shared front end of the target printers: it loads
  the JSON, types every expression, folds constants, and computes
  local usage and mutation.
- `cmd/json2zig`, `cmd/json2cpp` and `cmd/json2rust` print the
  engine in the target languages; the results and their runtimes
  live in `../zig1`, `../cpp1` and `../rust1`.
- `cmd/crosscheck` verifies the target drivers against the Go
  engine over the differential corpora; `cmd/godriver` runs the
  same driver protocol with the Go engine, for debugging.
- `probe/` is a second Vego package (JSON form: `probe.vego.json`)
  covering the subset constructs the engine never uses: range
  statements, division overflow, byte-slice conversion, comparable
  structs, evaluation order, spare-capacity zeroing. `cmd/proberef`
  prints its results with the Go original, and `cmd/probecheck`
  diffs each target's probe binary against them.

## Using it

```go
re, err := revera.New("([a-z]+)([0-9]*)")
groups, err := re.FindStringSubmatch("__abc12__")
// groups == []string{"abc12", "abc", "12"}
```

`revera_host.go` holds the idiomatic Go surface. It is a host file,
outside the subset, so every target writes the same thing for
itself.

- `New` compiles and returns `(*Regexp, error)`. `MustNew` panics
  instead, for patterns fixed at build time.
- The options are functions: `CaseInsensitive()`,
  `NewlineSensitive()`, `NoCaptures()`, `ShortestMatch()`, and
  `In(loc)` for a locale.
- The methods take the names of the standard `regexp` package:
  `MatchString`, `FindStringIndex`, `FindStringSubmatch`,
  `FindAllString`, `ReplaceAllString`, and the rest. Each one adds
  an `error` result, because a subject can exceed what the engine
  has capacity for. They return `nil` for a subject that does not
  match.
- `Error` implements the `error` interface.
- `OpenLocale("cs", "")` selects a CLDR locale for bracket
  expressions. The default is POSIX.
- `re.Contract(maxInput)` reports what one match can cost before it
  runs.

## API differences against go0

The subset has no methods, no interfaces, and no function values, so
the low-level surface below the host file changed shape without
changing behavior:

- Functions replace methods: `Exec(&re, subject, pmatch, eflags)`
  instead of `re.Exec(...)`. `Compile` returns the `Regexp` by
  value.
- Errors are values: `Error{Code, Pos}` with `ErrNone` meaning
  success, not a Go `error`.
- Flags and error codes are plain `uint32` and `int32` constants
  (`FlagICase`, `ExecNotBOL`, `ErrESpace`, ...). The numeric values
  match go0. The execution flags reach only this level; the
  methods above pass none.
- The callback API became an iterator: `MatchIterInit` and
  `MatchIterNext` drive the scan, and `ReplaceAll` builds on them.
- `ContractFor(&re, maxInput)` computes the resource contract. Its
  byte constants track this implementation's records (index arenas
  and open-addressing memo tables), so the figures differ from go0
  while covering the same allocations.
- Workspaces are fresh per `Exec` call instead of pooled. A
  compiled `Regexp` is never written after `Compile`, so concurrent
  `Exec` calls stay safe in Go.

One robustness improvement: the capture solver's arenas are capped.
Inputs that drive go0 into tens of gigabytes before its work limit
reports `ESpace` now report `ESpace` early, with bounded memory.

## Verification

`revera/differential_test.go` and `revera/locale_test.go` compare
this engine against go0 across random and fixed pattern corpora,
flags, locales (including multi-character collating elements),
UTF-8 handling, every locale operation, replacement, and iteration.
go0 is itself validated against an enumerating reference matcher and
the host `regcomp()`, so agreement carries that assurance over.

The JSON pipeline closes the loop:

```sh
go run ./cmd/vego2json -o revera.vego.json ./revera
go run ./cmd/json2go -o /tmp/engine_gen.go revera.vego.json
```

Regenerating the engine from the JSON and running the same test
suite against the regenerated file passes. The JSON therefore
carries the whole program, which is what the Rust, C++, and Zig
converters and the planned LEAN4 proof consume.

The target instantiations close a second loop. Each target builds
a driver speaking a small line protocol (see
`revera/driver_host.go`), and `cmd/crosscheck` feeds every driver
the corpora of the differential tests, comparing against the Go
engine line by line:

```sh
go run ./cmd/crosscheck ../zig1/zig-out/bin/driver \
    ../cpp1/driver ../rust1/target/release/driver
```
