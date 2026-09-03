# Revera for C++

This directory is the C++20 port of the Revera engine, a POSIX ERE regex engine with leftmost-longest matching.

`engine.hpp` and `engine.cpp` are generated from `revera.vego.json` at the repository root by `vegoc emit cpp`.
Do not edit them.

By contrast, the hand-written files are the public API and the minimal runtime that the Vego specification asks every target to supply.

## Using it

Include `revera.hpp` and link `revera.cpp` with `engine.cpp`:

```cpp
#include <iostream>
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
  A `Regex` is movable but not copyable.
- `find` and `captures` return `std::optional`; `find_all` and `capture_all` return `std::vector`.
  A subject that does not match gives an empty optional, not a failure.
- Engine failures throw `revera::Error`, which carries a `revera::Failure` code and an optional byte offset.
  Compilation offsets refer to the pattern, while replacement syntax offsets refer to the replacement text.
- `revera::Options` is a designated-initializer struct: `revera::Regex re("ab+", {.case_insensitive = true});`.
- `revera::Locale::open("cs")` selects a CLDR locale for bracket expressions.
  The default is POSIX.
- `re.contract(max_input)` reports what one search can cost, before it runs.
  Its capture breakdown selects either the one-pass walk or the general solver, never both.

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`, are not on this surface.
A caller who needs them includes `engine.hpp` and works in `namespace revera::engine`.

The CMake package in the parent directory builds this port as `librevera_cxx`, installs the header as `<revera/revera.hpp>`, and exports the target `Revera::CXX`.
That is the way to use it from another project.
See `../README.md`.

## Locale data

`revera.cpp` and `host.hpp` carry `data.bin`, the CLDR locale tables, inside the binary.
`locale_data.hpp` picks how the bytes get there.

When `REVERA_LOCALE_DATA_INC` is defined, it includes that file, which must list the bytes as `0x..,` items.
Otherwise it uses `#embed`, which needs clang 19 or gcc 15.
The Makefile relies on `#embed`.

The CMake build writes the byte list at configure time, so it works with any clang or gcc release that speaks C++20.
The arrays are `unsigned char`, because list-initialization of `char` from a value above 127 is a narrowing error in C++.

## Layout

- `revera.hpp` and `revera.cpp` are the public API.
  They hold the engine behind a pointer, so the header stays free of the runtime types.
- `locale_data.hpp` brings the bytes of `data.bin` into an array initializer.
- `engine.hpp` and `engine.cpp` are the generated engine, in `namespace revera::engine`.
  Regenerate them with `make generate`.
- `vg.hpp` is the runtime.
  It supplies the `Slice<T>` and `Str` value types with Go slice semantics, the conversion and comparison helpers, and the `Arena` allocator.
  It holds no state, so each thread can run its own engine instance.
- `driver.cpp` is the differential driver.
  It speaks the line protocol that `dev/internal/protocol/driver.go` defines.
- `host.hpp` is what `driver.cpp`, `bench.cpp` and `fuzz.cpp` share.
  It loads the base locale once and holds the hex and token helpers of the line protocols.
- `api_test.cpp` covers the public API.

## Build and verify

```sh
make all
make test
(cd ../../dev && go run ./internal/conformance/crosscheck ../native/cpp/driver)
(cd ../../dev && go run ./internal/conformance/probecheck ../native/cpp/probe)
```

`make all` also produces `probe`, the runner for the `vego/probe` package.
That package covers the subset constructs the engine never uses.
`probe` prints the same lines as `dev/internal/conformance/proberef`, and `probecheck` diffs them.

`make generate` regenerates the engine and the probe from the repository root.
It runs `cd ../../dev && go run ./cmd/generate -target cpp`.
The same thing from the repository root is `make generate GENERATION_TARGETS=cpp`.

`cd ../../dev && go run ./cmd/conform -backend ../native/cpp` runs the whole conformance kit against this port.

The Makefile invokes `$(CXX)`, normally `c++`, with `-std=c++20 -O2 -fwrapv` and keeps its assertions on.
Because this build uses `#embed`, `CXX` must resolve to Clang 19 or GCC 15 or later.
Override it explicitly when necessary, for example `make CXX=clang++-19 all`.

C++20 defines narrowing conversions to signed types as modular, and `-fwrapv` defines signed overflow.
Integer arithmetic therefore matches Go exactly.
The printer also casts every result narrower than 64 bits back to its Vego type, which defeats integer promotion.

## Bench, fuzz and checked builds

`make all` also builds `bench` and `fuzzcase`.

`bench` speaks the line protocol that `dev/internal/protocol/bench.go` defines.
A `B` command times one operation, compile, match or replace, and reports the bytes and allocations that the engine requests per operation.
The allocation figures come from the cumulative counters of `vg::Arena`, so they count the sizes the engine asks for and not what malloc rounds them up to.
The timings come from `std::chrono::steady_clock`.

```sh
printf 'P\nB m match 100 3 0 28617c62292b 616261626162 2d\n' | ./bench
```

`fuzz.cpp` holds `LLVMFuzzerTestOneInput`, the fuzz entry point that every target shares.
`dev/internal/protocol/fuzz.go` defines the input format.
One input selects the locale and the flags, then compiles, executes, replaces, iterates and asks for the contract.
It ignores every result, because freedom from crashes is the property.

`fuzzcase <packfile>` replays a pack of recorded inputs through the same entry point and prints the input count.
A record is a 4-byte little-endian length followed by that many bytes.

`make sanitize` builds `driver-asan`, `probe-asan` and `fuzzcase-asan` with AddressSanitizer and UndefinedBehaviorSanitizer at `-O1`.
Every report is fatal.
These binaries speak the same protocols as the plain ones, so `crosscheck` and `probecheck` accept them:

```sh
make sanitize
(cd ../../dev && go run ./internal/conformance/crosscheck ../native/cpp/driver-asan)
(cd ../../dev && go run ./internal/conformance/probecheck ../native/cpp/probe-asan)
```

`make libfuzzer` links `fuzz.cpp` with `-fsanitize=fuzzer,address,undefined`.
Apple clang ships no libFuzzer runtime, so on macOS the link fails with `libclang_rt.fuzzer_osx.a` not found.
The target is for clang on Linux, where `./libfuzzer corpus/` runs the fuzzer.

This port is distributed as part of the native archive described in `../README.md`.
That archive carries the project MIT license and the Unicode License v3 for the embedded locale data.
