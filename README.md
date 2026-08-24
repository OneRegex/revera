# re-vera2

This repository is the beginning of a clean-room POSIX.1-2024 Extended Regular
Expression (ERE) implementation. It currently contains the implementation
contract, source-backed studies of existing engines, and the first runtime
component: locale data for ERE character classes, case mappings, collating
elements, and equivalence classes.

It does not yet contain a complete regular-expression compiler or matcher.

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
- [`LOG.md`](LOG.md) and [`MISTAKES.md`](MISTAKES.md) record the work performed
  and corrections made during specification and implementation research.
- [`AGENTS.md`](AGENTS.md) contains repository guidance for automated
  contributors.

## Build and test

A C11 compiler is sufficient for the committed locale runtime and tests:

```sh
make test
```

Remove the ordinary test binaries with:

```sh
make clean
```

Regenerating `src/rv_locale_data.inc` additionally requires JDK 17 or later
and the pinned CLDR 48.2 artifacts. See
[`docs/LOCALE-TABLES.md`](docs/LOCALE-TABLES.md#reproduction) for the exact
inputs and command.
