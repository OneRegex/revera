# Vego

Vego is the strict Go subset that the Revera engine is written in, built for mechanical translation into other languages.
This module, `github.com/oneregex/revera/vego`, holds the language specification, its compiler and the `vegoc` command.
It has no dependency, and it does not depend on the engine.

[`SPECIFICATION.md`](SPECIFICATION.md) defines the subset and its JSON form, the Vego IR, in full.
[`schema/vego.schema.json`](schema/vego.schema.json) is the structural schema of the IR; the semantics stay in the prose.

## The vegoc command

```sh
go run ./cmd/vegoc check ../go
go run ./cmd/vegoc export -o ../revera.vego.json ../go
go run ./cmd/vegoc emit go   -o engine.go ../revera.vego.json
go run ./cmd/vegoc emit rust -o engine.rs ../revera.vego.json
go run ./cmd/vegoc emit zig  -o engine.zig ../revera.vego.json
go run ./cmd/vegoc emit cpp  -header engine.hpp -source engine.cpp -namespace revera::engine ../revera.vego.json
go run ./cmd/vegoc emit c    -header engine.h -source engine.c -prefix revera_eng ../revera.vego.json
go run ./cmd/vegoc version
```

`check` reports every construct outside the subset with a file and a line.
`export` runs the same check and writes the IR.
`emit` reads the IR and prints it in one target language.
An output flag left empty sends the text to standard output.

`vegoc` is the only executable the project publishes.
Install it with `go install github.com/oneregex/revera/vego/cmd/vegoc@latest`.

## Layout

- `compiler/` is the front end that every printer shares.
  It loads the IR, types every expression, folds constants, and computes local usage and mutation.
- `compiler/export/` is the checker and exporter.
  It reads a Go package, skips the `_host.go` and `_test.go` files, checks the subset rules and writes the IR.
- `compiler/printer/golang/`, `rust/`, `zig/`, `cpp/` and `c/` print the IR in each target language.
  The Go printer emits formatted Go source for inspection; the other printers lower the same typed IR into their target languages.
- `cmd/vegoc/` is the command line over the packages above.
- `probe/` is a second Vego program, the conformance program of a Vego backend.
  It covers the constructs the engine never uses: range statements, division overflow, byte-slice conversion, comparable structs, evaluation order, and spare-capacity zeroing.
  `probe/probe.vego.json` is its IR, and `probe/probe_host.go` prints the reference report that every backend must reproduce.

## Verification

```sh
GOWORK=off go test ./...
```

The printer tests compile their output where a toolchain is present: `clang` for C, `c++` for C++, `zig` for Zig, and `rustc` for Rust.
The Lean model in [`../lean`](../lean) gives the subset its formal semantics and checks the two IR files byte for byte.

## Writing a new backend

A backend starts from `../revera.vego.json`, `probe/probe.vego.json` and a `backend.json` manifest.
It needs a printer here, a hand-written runtime and public API in its own directory, and the driver, probe, bench and fuzzcase binaries that the manifest names.
[`../docs/CONFORMANCE-KIT.md`](../docs/CONFORMANCE-KIT.md) describes the manifest and the protocols.
`make conform` at the repository root runs generation freshness, release build, probe, corpus, stress, fuzz and Lean-data checks, and attempts each configured checked build.

## Versions

The Vego toolchain has its own SemVer, starting at 0.1.0, which `vegoc version` prints.
The IR carries its compatibility major in the `"vego": 1` field.
The Revera engine releases move on a third axis, shared by the five implementations.

## License

Vego is licensed under the repository's [MIT license](../LICENSE).
It embeds no Unicode or CLDR data.
