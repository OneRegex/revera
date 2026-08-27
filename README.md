# re-vera2

This repository is a clean-room POSIX.1-2024 Extended Regular Expression
(ERE) implementation. It contains the implementation contract, source-backed
studies of existing engines, a C locale runtime, and a complete Go
implementation of the contract.

The C side currently covers only the locale runtime: character classes, case
mappings, collating elements, and equivalence classes. The Go module in
[`go0/`](go0/) implements the full ERE language on top of the same data.

## Repository contents

### Specifications and design notes

- [`docs/POSIX-1-2024-ERE-SPECIFICATION.md`](docs/POSIX-1-2024-ERE-SPECIFICATION.md)
  is the clean-room, implementation-oriented POSIX.1-2024 Issue 8 ERE
  contract.
- [`docs/TRE-POSIX-ERE-DIVERGENCES.md`](docs/TRE-POSIX-ERE-DIVERGENCES.md)
  records source-backed differences between the contract and the pinned TRE
  tree.
- [`docs/ERE-IMPLEMENTATION-TECHNIQUES.md`](docs/ERE-IMPLEMENTATION-TECHNIQUES.md)
  extracts useful implementation techniques from TRE, RE2, and MinRX while
  keeping their semantic differences explicit.
- [`docs/LOCALE-TABLES.md`](docs/LOCALE-TABLES.md) documents the locale model,
  data coverage, and regeneration procedure.

### Locale implementation

- [`src/rv_locale.h`](src/rv_locale.h) defines the locale API.
- [`src/rv_locale.c`](src/rv_locale.c) implements allocation-free locale,
  character-class, case-mapping, collating-element, and primary-equivalence
  lookups.
- `src/rv_locale_data.inc` contains generated CLDR 48.2 and Unicode 17.0.0
  tables. The data covers 1,122 CLDR locales and their available collation
  types, plus the `C` and `POSIX` aliases.
- [`tools/GenerateLocaleData.java`](tools/GenerateLocaleData.java) and
  [`tools/generate-locale-data.sh`](tools/generate-locale-data.sh) reproduce
  the generated tables from pinned CLDR artifacts.
- [`tests/test_locale.c`](tests/test_locale.c) exercises the public API, and
  [`tests/test_locale_internal.c`](tests/test_locale_internal.c) checks the
  generated tables and their invariants.

### Go implementation

- [`go0/`](go0/) holds `revera`, a complete Go implementation of the ERE
  contract: parser, engine, capture resolution, resource contracts, and an
  embedded copy of the locale data.
- [`go0/README.md`](go0/README.md) documents its API, input model, and
  conformance testing. [`go0/NOTES.md`](go0/NOTES.md) records the chosen
  outcomes for undefined and unspecified constructs.

### Vego pipeline and LEAN4 model

- [`go1/`](go1/) rewrites the engine in Vego, a mechanically
  translatable Go subset, and exports it to JSON
  (`go1/revera.vego.json`). The `cmd/json2go`, `cmd/json2rust`,
  `cmd/json2zig` and `cmd/json2cpp` printers generate the engines in
  [`rust1/`](rust1/), [`zig1/`](zig1/) and [`cpp1/`](cpp1/);
  `cmd/crosscheck` and `cmd/probecheck` verify them differentially.
  Each target adds a small hand-written public API over the
  generated engine, in the shape its own language expects. Their
  READMEs show it.
- [`lean/`](lean/) holds the LEAN4 model of Vego: a formal semantics
  for the subset and machine-checked theorems that the exact JSON
  artifacts are well formed and reproduce the Go reference outputs,
  trap-free, and that no execution it replays from the differential
  corpus ever exceeds its resource contract. [`lean/README.md`](lean/README.md)
  states precisely what is proved.

### Reference engines

The `ref/` directory contains independent upstream trees used as evidence and
design references. They are pinned to these revisions:

- `ref/tre` at `71bfcaf0af3994384987c6c2679ed7d078ffe189` is the
  POSIX-oriented implementation and divergence baseline.
- `ref/re2` at `972a15cedd008d846f1a39b2e88ce48d7f166cbd` is a
  linear-time engine design reference.
- `ref/minrx` at `d13610cdf983337d32b5e07a46da69e40ec5adb0` is a compact
  structured-NFA design reference.

Project changes belong outside `ref/` unless a reference revision is being
updated deliberately.

### Supporting material

- [`Makefile`](Makefile) builds and runs the locale tests.
- [`LICENSES/Unicode-3.0.txt`](LICENSES/Unicode-3.0.txt) is the license for the
  generated Unicode and CLDR-derived data.
- [`LOG.md`](LOG.md), [`MISTAKES.md`](MISTAKES.md), and
  [`api-faq.md`](api-faq.md) record the work performed and the corrections
  made along the way.

## Build and test

A C11 compiler is sufficient for the committed locale runtime and tests:

```sh
make test
```

Remove the ordinary test binaries with:

```sh
make clean
```

The Go module tests only need a Go toolchain:

```sh
cd go0 && go test ./...
```

Regenerating `src/rv_locale_data.inc` additionally requires JDK 17 or later
and the pinned CLDR 48.2 artifacts. See
[`docs/LOCALE-TABLES.md`](docs/LOCALE-TABLES.md#reproduction) for the exact
inputs and command.
