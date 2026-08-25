# cpp1: the revera engine in C++

This directory is the C++ instantiation of the Vego engine from
`go1/`. `engine.hpp` and `engine.cpp` are generated from
`go1/revera.vego.json` by `go1/cmd/json2cpp`; do not edit them.
The hand-written files are the minimal runtime the Vego spec
requires each target to supply.

## Layout

- `engine.hpp` / `engine.cpp` are the generated engine, in
  `namespace revera`. Regenerate with `make generate`, or:

  ```sh
  cd ../go1 && go run ./cmd/json2cpp \
      -hpp ../cpp1/engine.hpp -cpp ../cpp1/engine.cpp revera.vego.json
  ```

- `vg.hpp` is the runtime: `Slice<T>` and `Str` value types with
  Go slice-header semantics, conversion and comparison helpers,
  and three arenas (persistent locale data, per-pattern,
  per-operation scratch).
- `driver.cpp` is the differential driver. It speaks the line
  protocol defined in `go1/revera/driver_host.go` and embeds
  `data.bin` with `#embed`.

## Build and verify

```sh
make all
cd ../go1 && go run ./cmd/crosscheck ../cpp1/driver
cd ../go1 && go run ./cmd/probecheck ../cpp1/probe
```

`make all` also produces `probe`, the runner for `go1/probe`: a
second Vego package covering the constructs the engine never
uses. Because C++ leaves argument and operand order unspecified
while Go fixes it left to right, the printer pins side-effecting
subexpressions into ordered temporaries.

The build uses `-std=c++20 -O2 -fwrapv` and keeps asserts on.
C++20 defines narrowing conversions to signed types as modular,
and -fwrapv defines signed overflow, so integer arithmetic matches
Go exactly; results narrower than 64 bits are also cast back to
their Vego type to defeat integer promotion.
