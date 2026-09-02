# Revera for C11

This directory is the C11 port of the Revera engine, a POSIX ERE regex engine with leftmost-longest matching.

`engine.h` and `engine.c` are generated from `revera.vego.json` at the repository root by `vegoc emit c`.
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

The API uses opaque locale, regular-expression and iterator handles.
Functions report engine or input failures through a null pointer or `false`, and the calls that accept a `revera_error` fill it with a status, optional byte offset and static message.

Offsets and string lengths are byte counts.
A `revera_match` stores offsets into the subject rather than a pointer to it.

An iterator borrows its regular expression and subject until `revera_iterator_free`.
By contrast, replacement functions return memory that the caller frees with `free`.

- `revera_matches`, `revera_find` and `revera_captures` perform one search.
- `revera_iterator_new` and `revera_iterator_next` enumerate non-overlapping matches.
- `revera_replace_all` and `revera_replace_first_n` apply `&` and `\1` through `\9` replacements.
- `revera_locale_open` selects embedded locale data, and `revera_contract_for` reports resource figures before a search.

The CMake package in the parent directory builds this port as `librevera_c`, installs the header as `<revera/revera.h>`, and exports the target `Revera::C`.
That is the way to use it from another project.
See `../README.md`.

## Locale data

`revera.c` and `host.h` carry `data.bin`, the CLDR locale tables, inside the binary.
`locale_data.h` picks how the bytes get there.

When `REVERA_LOCALE_DATA_INC` is defined, it includes that file, which must list the bytes as `0x..,` items.
Otherwise it uses `#embed`, which needs clang 19 or gcc 15.
The Makefile relies on `#embed`.

The CMake build writes the byte list at configure time, so it works with any clang or gcc release that speaks C11.
The arrays are `unsigned char`, so a byte above 127 in the list raises no overflow warning from gcc.

## Layout

- `revera.h` and `revera.c` are the public API.
  They keep the generated engine behind opaque handles.
- `locale_data.h` brings the bytes of `data.bin` into an array initializer.
- `engine.h` and `engine.c` are the generated engine.
  Regenerate them with `make generate`.
- `vg.h` is the runtime.
  It supplies `vg_str`, the arena allocator, and the helpers for Go arithmetic.
  The generated header supplies one slice type and helper family for each element type that the program uses.
- `driver.c` is the differential driver.
  It speaks the line protocol that `dev/internal/protocol/driver.go` defines.
- `host.h` is what `driver.c`, `bench.c`, and `fuzz.c` share.
  It loads the base locale once and holds the token helpers of the line protocols.
- `api_test.c` covers the public API.

## Build and verify

```sh
make all
make test
(cd ../../dev && go run ./internal/conformance/crosscheck ../native/c/driver)
(cd ../../dev && go run ./internal/conformance/probecheck ../native/c/probe)
```

`make all` also produces `probe`, the runner for the `vego/probe` package.
That package covers the Vego constructs the engine does not use.
`probe` prints the same lines as `dev/internal/conformance/proberef`, and `probecheck` diffs them.

`make generate` regenerates the engine and the probe from the repository root.
It runs `cd ../../dev && go run ./cmd/generate -target c`.
The same thing from the repository root is `make generate GENERATION_TARGETS=c`.

`cd ../../dev && go run ./cmd/conform -backend ../native/c` runs the whole conformance kit against this port.

The Makefile invokes `$(CC)`, normally `cc`, with `-std=c11 -O2 -fwrapv` and keeps its assertions on.
Because this build uses `#embed`, `CC` must resolve to Clang 19 or GCC 15 or later.
Override it explicitly when necessary, for example `make CC=clang-19 all`.

The printer casts every result narrower than 64 bits back to its Vego type, which defeats integer promotion.
It lowers signed division overflow and signed left shifts through defined unsigned arithmetic.

## Bench, fuzz, and checked builds

`make all` also builds `bench` and `fuzzcase`.

`bench` speaks the line protocol that `dev/internal/protocol/bench.go` defines.
A `B` command times compilation, matching, or replacement and reports the bytes and allocations that the engine requests per operation.
The allocation figures come from the cumulative counters of `vg_arena`.

```sh
printf 'P\nB m match 100 3 0 28617c62292b 616261626162 2d\n' | ./bench
```

`fuzz.c` holds `LLVMFuzzerTestOneInput`, the fuzz entry point that every target shares.
`dev/internal/protocol/fuzz.go` defines the input format.
One input selects the locale and flags, then compiles, executes, replaces, iterates, and asks for the contract.
It ignores every result because freedom from crashes is the property.

`fuzzcase <packfile>` replays a pack of recorded inputs through the same entry point and prints the input count.
A record is a 4-byte little-endian length followed by that many bytes.

`make sanitize` builds `driver-asan`, `probe-asan`, and `fuzzcase-asan` with AddressSanitizer and UndefinedBehaviorSanitizer at `-O1`.
Every report is fatal.
These binaries speak the same protocols as the plain ones.

```sh
make sanitize
(cd ../../dev && go run ./internal/conformance/crosscheck ../native/c/driver-asan)
(cd ../../dev && go run ./internal/conformance/probecheck ../native/c/probe-asan)
```

`make libfuzzer` links `fuzz.c` with `-fsanitize=fuzzer,address,undefined`.
Apple clang does not ship a libFuzzer runtime, so this target is for Clang on Linux.

This port is distributed as part of the native archive described in `../README.md`.
That archive carries the project MIT license and the Unicode License v3 for the embedded locale data.
