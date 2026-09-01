# c1: the revera engine in C11

This directory is the C11 instantiation of the Vego engine from `go1/`.

`engine.h` and `engine.c` are generated from `go1/revera.vego.json` by `go1/cmd/json2c`.
Do not edit them.
The hand-written files are the public API and the minimal runtime that the Vego specification asks every target to supply.

## Using it

Include `revera.h` and link `revera.c` with `engine.c`:

```c
#include "revera.h"

const char pattern[] = "([a-z]+)([0-9]*)";
revera_error error;
revera_regex *re = revera_compile(pattern, sizeof(pattern) - 1, NULL, &error);
if (re != NULL) {
    revera_match groups[3];
    if (revera_captures(re, "__abc12__", 9, groups, 3, &error)) {
        /* groups[1] names the bytes "abc". */
    }
    revera_regex_free(re);
}
```

`revera.h` includes standard headers only.
The generated engine, its arenas, and its numeric flags stay out of sight.

The API uses opaque locale and regular-expression handles.
Fallible operations return a status or write a `revera_error`.
Offsets and string lengths are byte counts.
A match borrows its subject, while replacement functions return memory that the caller frees.

## Layout

- `revera.h` and `revera.c` are the public API.
  They embed `data.bin` and keep the generated engine behind opaque handles.
- `engine.h` and `engine.c` are the generated engine.
  Regenerate them with `make generate`.
- `vg.h` is the runtime.
  It supplies `vg_str`, the arena allocator, and the helpers for Go arithmetic.
  The generated header supplies one slice type and helper family for each element type that the program uses.
- `driver.c` is the differential driver.
  It speaks the line protocol that `go1/revera/driver_host.go` defines.
- `host.h` is what `driver.c`, `bench.c`, and `fuzz.c` share.
  It embeds `data.bin`, loads the base locale once, and holds the token helpers of the line protocols.
- `api_test.c` covers the public API.

## Build and verify

```sh
make all
make test
cd ../go1 && go run ./cmd/crosscheck ../c1/driver
cd ../go1 && go run ./cmd/probecheck ../c1/probe
```

`make all` also produces `probe`, the runner for the `go1/probe` package.
That package covers the Vego constructs the engine does not use.

The build uses Clang with `-std=c11 -O2 -fwrapv` and keeps its assertions on.
The printer casts every result narrower than 64 bits back to its Vego type, which defeats integer promotion.
It lowers signed division overflow and signed left shifts through defined unsigned arithmetic.

## Bench, fuzz, and checked builds

`make all` also builds `bench` and `fuzzcase`.

`bench` speaks the line protocol that `go1/revera/bench_host.go` defines.
A `B` command times compilation, matching, or replacement and reports the bytes and allocations that the engine requests per operation.
The allocation figures come from the cumulative counters of `vg_arena`.

```sh
printf 'P\nB m match 100 3 0 28617c62292b 616261626162 2d\n' | ./bench
```

`fuzz.c` holds `LLVMFuzzerTestOneInput`, the fuzz entry point that every target shares.
One input selects the locale and flags, then compiles, executes, replaces, iterates, and asks for the contract.
It ignores every result because freedom from crashes is the property.
`fuzzcase <packfile>` replays a pack of recorded inputs through the same entry point and prints the input count.
A record is a 4-byte little-endian length followed by that many bytes.

`make sanitize` builds `driver-asan`, `probe-asan`, and `fuzzcase-asan` with AddressSanitizer and UndefinedBehaviorSanitizer at `-O1`.
Every report is fatal.
These binaries speak the same protocols as the plain ones.

```sh
make sanitize
cd ../go1 && go run ./cmd/crosscheck ../c1/driver-asan
cd ../go1 && go run ./cmd/probecheck ../c1/probe-asan
```

`make libfuzzer` links `fuzz.c` with `-fsanitize=fuzzer,address,undefined`.
Apple clang does not ship a libFuzzer runtime, so this target is for Clang on Linux.
