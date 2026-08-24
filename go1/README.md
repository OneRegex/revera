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

## API differences against go0

The subset has no methods, no interfaces, and no function values, so
the surface changed shape without changing behavior:

- Functions replace methods: `Exec(&re, subject, pmatch, eflags)`
  instead of `re.Exec(...)`. `Compile` returns the `Regexp` by
  value.
- Errors are values: `Error{Code, Pos}` with `ErrNone` meaning
  success, not a Go `error`.
- Flags and error codes are plain `uint32` and `int32` constants
  (`FlagICase`, `ExecNotBOL`, `ErrESpace`, ...). The numeric values
  match go0.
- The callback API became an iterator: `MatchIterInit` and
  `MatchIterNext` drive the scan, and `ReplaceAll` builds on them.
  The host file restores `MatchAll` and `ReplaceAllFunc` for Go
  users.
- `ContractFor(&re, maxInput)` computes the resource contract; the
  host file keeps `CompileWithContract`. The contract's byte
  constants track this implementation's records (index arenas and
  open-addressing memo tables), so the figures differ from go0
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
carries the whole program, which is what the planned Rust, C++, and
Zig converters and the LEAN4 proof consume.
