# cpp1: the revera engine in C++

This directory is the C++ instantiation of the Vego engine from
`go1/`. `engine.hpp` and `engine.cpp` are generated from
`go1/revera.vego.json` by `go1/cmd/json2cpp`; do not edit them.
The hand-written files are the public API and the minimal runtime
the Vego spec requires each target to supply.

## Using it

Include `revera.hpp` and link `revera.cpp` with `engine.cpp`:

```cpp
#include "revera.hpp"

revera::Regex re("([a-z]+)([0-9]*)");
if (auto caps = re.captures("__abc12__")) {
    std::cout << (*caps)[1]->str() << "\n";  // abc
}
```

`revera.hpp` includes standard headers only. The engine, its
arenas and its numeric flags stay out of sight.

- `revera::Regex` compiles a pattern and searches. Every search is
  const and keeps no state between calls, so one `Regex` serves
  any number of threads.
- `find`, `captures` and their `_all` forms return `std::optional`
  and `std::vector`. A subject that does not match is an empty
  optional, not a failure.
- Compilation and search failures throw `revera::Error`, which
  carries a `revera::Failure` code and the byte offset in the
  pattern.
- `revera::Options` is a designated-initializer struct:
  `revera::Regex re("ab+", {.case_insensitive = true});`.
- `revera::Locale::open("cs")` selects a CLDR locale for bracket
  expressions. The default is POSIX.
- `re.contract(max_input)` reports what one search can cost before
  it runs.

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`,
are not on this surface. A caller who needs them includes
`engine.hpp` and works in `namespace revera::engine` directly.

## Layout

- `engine.hpp` / `engine.cpp` are the generated engine, in
  `namespace revera::engine`. Regenerate with `make generate`, or:

  ```sh
  cd ../go1 && go run ./cmd/json2cpp \
      -hpp ../cpp1/engine.hpp -cpp ../cpp1/engine.cpp \
      -ns revera::engine revera.vego.json
  ```

- `revera.hpp` / `revera.cpp` are the public API. They embed
  `data.bin` with `#embed` and hold the engine behind a pointer, so
  the header stays free of the runtime types.
- `vg.hpp` is the runtime: `Slice<T>` and `Str` value types with
  Go slice-header semantics, conversion and comparison helpers,
  and the `Arena` allocator. It holds no state. Every generated
  function that allocates takes an `Arena&` as its first
  parameter, `mem`; the engine is re-entrant, and each thread can
  run its own instance.
- `driver.cpp` is the differential driver. It speaks the line
  protocol defined in `go1/revera/driver_host.go` and embeds
  `data.bin` with `#embed`. It owns three arenas (persistent
  locale data, per-pattern, per-operation scratch) and passes the
  right one to each engine call.
- `api_test.cpp` covers the public API.

## Build and verify

```sh
make all
make test
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
