# Locale runtime

This directory holds the C locale runtime and the generated CLDR tables that every Revera engine embeds.

- `rv_locale.h` is the locale API.
- `rv_locale.c` answers class, case, collating-element and primary-equivalence lookups without allocating.
- `rv_locale_data.inc` holds the generated CLDR 48.2 and Unicode 17.0.0 tables.
  They cover 1,122 CLDR locales with their collation types, plus the `C` and `POSIX` aliases.
- `tools/GenerateLocaleData.java` and `tools/generate-locale-data.sh` reproduce those tables from pinned CLDR artifacts.
- `tests/` exercises the public API and checks the invariants of the generated tables.

[`../docs/LOCALE-TABLES.md`](../docs/LOCALE-TABLES.md) documents the locale model, the data coverage, and the reproduction steps.

## API

Include `rv_locale.h` and compile or link `rv_locale.c`.
The API works on Unicode scalar values and explicit scalar sequences; UTF-8 decoding stays at the caller's boundary.
`rv_locale_open` accepts ASCII-case-insensitive CLDR names with `-` or `_` separators, an optional `.UTF-8` suffix, and either a separate collation type or an `@collation=` modifier.
The `C` and `POSIX` names select the built-in POSIX locale.
Locale values are small caller-owned structs, lookups allocate no memory, and returned locale names point into static generated data.

## Build and test

A C11 compiler is enough.
The Makefile uses `CC`, `CPPFLAGS` and `CFLAGS`, so each can be overridden in the usual way:

```sh
make test
```

`make test` at the repository root runs the same thing.

## The data blob

The engines do not read these tables directly.
`dev/internal/genlocale` validates and compiles `rv_locale_data.inc` into `data.bin`, a compact little-endian section blob, and writes every checked-in copy:

```sh
cd ../dev && go run ./internal/genlocale
```

The copies are `go/data.bin`, `dev/internal/reference/locale/data.bin`, `rust/src/data.bin`, `zig/src/data.bin`, `native/c/data.bin` and `native/cpp/data.bin`.
The conformance kit checks that they stay byte-identical, and that `lean/data/localedata.hex` matches them.
From `dev/`, write selected outputs by passing the input followed by one or more output paths: `go run ./internal/genlocale INPUT.inc OUTPUT.bin...`.

## Regenerating the tables

A rebuild of `rv_locale_data.inc` needs JDK 17 or later and the pinned CLDR 48.2 artifacts.
Run the script from this directory:

```sh
tools/generate-locale-data.sh cldr-common-48.2.zip cldr-tools-48.2.jar "$JAVA_HOME"
```

It verifies the SHA-512 of both inputs, compiles the generator, and writes `rv_locale_data.inc`.
An optional fourth argument selects a different output path.
Run `cd ../dev && go run ./internal/genlocale` afterwards to rebuild every `data.bin` copy.

## License

The locale runtime and generator use the project MIT license in [`../LICENSE`](../LICENSE).
The generated CLDR and Unicode tables use the Unicode License v3 in [`../LICENSES/Unicode-3.0.txt`](../LICENSES/Unicode-3.0.txt).
