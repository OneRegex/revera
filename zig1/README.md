# zig1: the revera engine in Zig

This directory is the Zig instantiation of the Vego engine from
`go1/`. `src/engine.zig` is generated from `go1/revera.vego.json`
by `go1/cmd/json2zig`; do not edit it. The hand-written files are
the minimal runtime the Vego spec requires each target to supply.

## Layout

- `src/engine.zig` is the generated engine. Regenerate with:

  ```sh
  cd ../go1 && go run ./cmd/json2zig -o ../zig1/src/engine.zig revera.vego.json
  ```

- `src/vg.zig` is the runtime: the `Slice` and `Str` value types
  with Go slice-header semantics, the integer conversion helper,
  and string comparison. It holds no state. Every generated
  function that allocates takes an allocator as its first
  parameter, `mem`; the engine is re-entrant, and each thread can
  run its own instance.
- `src/main.zig` is the differential driver. It speaks the line
  protocol defined in `go1/revera/driver_host.go`. It owns three
  arenas (persistent locale data, per-pattern, per-operation
  scratch) and passes the right one to each engine call.
- `src/data.bin` is the CLDR locale blob, embedded at build time.

## Build and verify

```sh
zig build -Drelease
cd ../go1 && go run ./cmd/crosscheck ../zig1/zig-out/bin/driver
cd ../go1 && go run ./cmd/probecheck ../zig1/zig-out/bin/probe
```

The build also produces `probe`, the runner for `go1/probe`: a
second Vego package covering the constructs the engine never
uses (range statements, division overflow, byte conversions,
comparable structs, evaluation order).

The crosscheck corpus mirrors the go1 differential tests: random
patterns over every flag set, the fixed corpus, the cs
multi-element locale, replacement, iteration, contracts, and
locale case-map sweeps. The Go engine computes the expected
output in-process; the driver must agree on every line.

The build defaults to ReleaseSafe, and the runtime keeps its
bounds asserts in every mode. Out-of-range indexing aborts, which
is exactly the Go behavior the spec requires.
