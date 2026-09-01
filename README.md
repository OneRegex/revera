# re-vera2

This repository is a clean-room implementation of the POSIX.1-2024 Extended Regular Expression language.

It holds one written contract, one reference engine in Go, and the same engine in Rust, Zig, C++, and C11.
The five engines come from a single source, through a mechanical pipeline.
A Lean 4 model proves what that pipeline preserves.
A C locale runtime and its generated CLDR tables supply the character classes, case mappings and collating data underneath.

## Repository contents

### Specifications and design notes

- [`docs/POSIX-1-2024-ERE-SPECIFICATION.md`](docs/POSIX-1-2024-ERE-SPECIFICATION.md) is the contract every engine here implements.
  It restates POSIX.1-2024 Issue 8 for an implementer, and it decides the outcomes POSIX leaves open.
- [`docs/TRE-POSIX-ERE-DIVERGENCES.md`](docs/TRE-POSIX-ERE-DIVERGENCES.md) records where the pinned TRE tree differs from the contract, with the source lines that prove it.
- [`docs/ERE-IMPLEMENTATION-TECHNIQUES.md`](docs/ERE-IMPLEMENTATION-TECHNIQUES.md) collects techniques from TRE, RE2 and MinRX, and marks the semantics each one assumes.
- [`docs/LOCALE-TABLES.md`](docs/LOCALE-TABLES.md) documents the locale model, the data coverage, and how to reproduce the tables.
- [`docs/CONFORMANCE-KIT.md`](docs/CONFORMANCE-KIT.md) describes the backend conformance kit and the manifest a new target provides.
- [`docs/BENCHMARKS.md`](docs/BENCHMARKS.md) describes the cross-language benchmarks, the profiling workflow, and records the current figures.

### Locale runtime

- [`src/rv_locale.h`](src/rv_locale.h) is the locale API.
- [`src/rv_locale.c`](src/rv_locale.c) answers class, case, collating-element and primary-equivalence lookups without allocating.
- `src/rv_locale_data.inc` holds the generated CLDR 48.2 and Unicode 17.0.0 tables.
  They cover 1,122 CLDR locales with their collation types, plus the `C` and `POSIX` aliases.
- [`tools/GenerateLocaleData.java`](tools/GenerateLocaleData.java) and [`tools/generate-locale-data.sh`](tools/generate-locale-data.sh) reproduce those tables from pinned CLDR artifacts.
- [`tests/`](tests/) exercises the public API and checks the invariants of the generated tables.

### Engines

- [`go0/`](go0/) is the reference engine: parser, matcher, capture resolution, resource contracts, and an embedded copy of the locale data.
  A reference matcher that enumerates every parse checks its answers, and the host `regcomp()` checks them again.
- [`go1/`](go1/) rewrites that engine in Vego, a Go subset built for mechanical translation, and exports it as `revera.vego.json`.
- [`rust1/`](rust1/), [`zig1/`](zig1/), [`cpp1/`](cpp1/), and [`c1/`](c1/) are the engine printed into each target language, with a hand-written public API in the shape that language expects.
- [`lean/`](lean/) is the Lean 4 model of Vego.
  It gives the subset a formal semantics.
  It proves that the shipped JSON artifacts are well formed.
  It also checks that the embedded probe and selected corpus executions reproduce the Go reference outputs and stay inside their resource contracts.

Each directory has a README that states its own API and how to verify it.

### Reference engines

`ref/` holds independent upstream trees, used as evidence and as design references.
They are pinned to these revisions:

- `ref/tre` at `71bfcaf0af3994384987c6c2679ed7d078ffe189` is the POSIX-oriented implementation and the divergence baseline.
- `ref/re2` at `972a15cedd008d846f1a39b2e88ce48d7f166cbd` is a linear-time engine design reference.
- `ref/minrx` at `d13610cdf983337d32b5e07a46da69e40ec5adb0` is a compact structured-NFA design reference.

Project changes belong outside `ref/`, unless a reference revision moves on purpose.

### Supporting material

- [`Makefile`](Makefile) builds and runs the locale tests.
- [`LICENSES/Unicode-3.0.txt`](LICENSES/Unicode-3.0.txt) is the license of the generated Unicode and CLDR data.

## Build and test

A C11 compiler is enough for the locale runtime and its tests:

```sh
make test
```

The Go engines need only a Go toolchain:

```sh
(cd go0 && go test ./...)
(cd go1 && go test ./...)
```

Regenerate every Vego JSON and Rust, Zig, C++, and C11 source artifact with one command:

```sh
make generate
make check-generated
```

`check-generated` renders into an isolated directory under `tmp/`, compares file contents, and exits nonzero without modifying checked generated artifacts when an artifact is stale or missing.
Set `GENERATION_TARGETS=rust`, `zig`, `cpp`, `c`, or a comma-separated selection to limit the target sources.
The two shared JSON files are always included.

The Rust, Zig, C++, and C11 engines each build and verify from their own directory.
Their READMEs give the commands, including the differential run against the Go engine.

One command decides whether a backend is conformant, and one command benchmarks every engine:

```sh
make conform
make bench
make size
make profile
```

`conform` builds each backend from its `backend.json`, then runs the probe, the differential corpus, stress rounds, the fuzz seed pack, the sanitizer and debug builds, and checks that the Lean data matches the corpus.
`bench` prints compile, match, replacement and difficult-pattern figures with allocation counts, side by side for Go, Rust, Zig, C++, and C11.
`size` reports the generated code size per target, and `profile` writes CPU and allocation profiles of the Go engine.

A rebuild of `src/rv_locale_data.inc` also needs JDK 17 or later and the pinned CLDR 48.2 artifacts.
[`docs/LOCALE-TABLES.md`](docs/LOCALE-TABLES.md#reproduction) gives the exact inputs and command.
