# rust1: the revera engine in Rust

This directory is the Rust instantiation of the Vego engine from
`go1/`. `src/engine.rs` is generated from `go1/revera.vego.json`
by `go1/cmd/json2rust`; do not edit it. The hand-written files are
the minimal runtime the Vego spec requires each target to supply.

## Layout

- `src/engine.rs` is the generated engine. Regenerate with:

  ```sh
  cd ../go1 && go run ./cmd/json2rust -o ../rust1/src/engine.rs revera.vego.json
  ```

- `src/vg.rs` is the runtime: Copy `Slice<T>` and `Str` header
  types with Go slice semantics, conversion and comparison
  helpers, and three arenas (persistent locale data, per-pattern,
  per-operation scratch). Vego views can alias, which Rust
  references cannot express, so slices lower to raw pointer and
  length pairs, the route the Vego spec names for generated code.
  Every access stays bounds-checked.
- `src/main.rs` is the differential driver. It speaks the line
  protocol defined in `go1/revera/driver_host.go` and embeds
  `data.bin` with `include_bytes!`.

## Build and verify

```sh
cargo build --release
cd ../go1 && go run ./cmd/crosscheck ../rust1/target/release/driver
cd ../go1 && go run ./cmd/probecheck ../rust1/target/release/probe
```

The build also produces `probe`, the runner for `go1/probe`: a
second Vego package covering the constructs the engine never
uses. One known limit: a call passing the same struct variable
through two pointer arguments is valid Vego but rejects at Rust
compile time (E0499), loudly rather than silently.

Both profiles turn overflow checks off, so integer arithmetic
wraps like Go. The runtime uses `assert!`, not `debug_assert!`,
so out-of-range indexing aborts in every profile, exactly the Go
behavior the spec requires.
