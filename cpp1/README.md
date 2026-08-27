# cpp1: the revera engine in C++

This directory is the C++ instantiation of the Vego engine from `go1/`.

`engine.hpp` and `engine.cpp` are generated from `go1/revera.vego.json` by `go1/cmd/json2cpp`.
Do not edit them.
The hand-written files are the public API and the minimal runtime that the Vego specification asks every target to supply.

## Using it

Include `revera.hpp` and link `revera.cpp` with `engine.cpp`:

```cpp
#include "revera.hpp"

revera::Regex re("([a-z]+)([0-9]*)");
if (auto caps = re.captures("__abc12__")) {
    std::cout << (*caps)[1]->str() << "\n";  // abc
}
```

`revera.hpp` includes standard headers only.
The engine, its arenas and its numeric flags stay out of sight.

- `revera::Regex` compiles a pattern and searches.
  Every search is const and keeps no state between calls, so one `Regex` serves any number of threads.
- `find`, `captures` and their `_all` forms return `std::optional` and `std::vector`.
  A subject that does not match gives an empty optional, not a failure.
- Compilation and search failures throw `revera::Error`, which carries a `revera::Failure` code and the byte offset in the pattern.
- `revera::Options` is a designated-initializer struct: `revera::Regex re("ab+", {.case_insensitive = true});`.
- `revera::Locale::open("cs")` selects a CLDR locale for bracket expressions.
  The default is POSIX.
- `re.contract(max_input)` reports what one search can cost, before it runs.

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`, are not on this surface.
A caller who needs them includes `engine.hpp` and works in `namespace revera::engine`.

## Layout

- `revera.hpp` and `revera.cpp` are the public API.
  They embed `data.bin` and hold the engine behind a pointer, so the header stays free of the runtime types.
- `engine.hpp` and `engine.cpp` are the generated engine, in `namespace revera::engine`.
  Regenerate them with `make generate`.
- `vg.hpp` is the runtime.
  It supplies the `Slice<T>` and `Str` value types with Go slice semantics, the conversion and comparison helpers, and the `Arena` allocator.
  It holds no state, so each thread can run its own engine instance.
- `driver.cpp` is the differential driver.
  It speaks the line protocol that `go1/revera/driver_host.go` defines.
- `api_test.cpp` covers the public API.

## Build and verify

```sh
make all
make test
cd ../go1 && go run ./cmd/crosscheck ../cpp1/driver
cd ../go1 && go run ./cmd/probecheck ../cpp1/probe
```

`make all` also produces `probe`, the runner for the `go1/probe` package.
That package covers the subset constructs the engine never uses.

The build uses `-std=c++20 -O2 -fwrapv` and keeps its asserts on.
C++20 defines narrowing conversions to signed types as modular, and `-fwrapv` defines signed overflow.
Integer arithmetic therefore matches Go exactly.
The printer also casts every result narrower than 64 bits back to its Vego type, which defeats integer promotion.
