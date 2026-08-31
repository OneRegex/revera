# rust1: the revera engine in Rust

This directory is the Rust instantiation of the Vego engine from `go1/`.

`src/engine.rs` is generated from `go1/revera.vego.json` by `go1/cmd/json2rust`.
Do not edit it.
The hand-written files are the public API and the minimal runtime that the Vego specification asks every target to supply.

## Using it

```rust
let re = revera::Regex::new("([a-z]+)([0-9]*)")?;
let caps = re.captures("__abc12__")?.expect("a match");
assert_eq!(&caps[1], "abc");
```

`src/lib.rs` is the crate root and the whole public surface.

- `Regex` compiles a pattern and searches.
  A search takes `&self` and keeps no state between calls, so a `Regex` is `Send` and `Sync`.
- `find` and `captures` return `Result<Option<_>>`.
  A subject that does not match gives `None`, and only a real failure gives `Err`.
  `Match` borrows the subject and offers `as_str`, `range`, `start` and `end`.
- `find_iter` and `captures_iter` are lazy iterators over the non-overlapping matches.
- `Error` implements `std::error::Error`.
  `Error::kind` returns an `ErrorKind` variant, and `Error::offset` returns the byte offset in the pattern.
- `RegexBuilder` carries the options: `RegexBuilder::new("ab+").case_insensitive(true).build()?`.
- `Locale::open("cs", "")` selects a CLDR locale for bracket expressions.
  The default is POSIX.
- `re.contract(max_input)` reports what one search can cost, before it runs.

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`, are not exposed by this crate.

## Layout

- `src/lib.rs` is the public API.
  It embeds `data.bin` with `include_bytes!`.
- `src/engine.rs` is the generated engine.
  Regenerate it with:

  ```sh
  cd ../go1
  go run ./cmd/revera generate -target rust
  ```

- `src/vg.rs` is the runtime.
  It supplies the `Copy` `Slice<T>` and `Str` header types with Go slice semantics, the conversion and comparison helpers, and the `Arena` allocator.
  `Arena` is `!Sync`, so the compiler refuses to share one between threads.
  A Vego view can alias, which a Rust reference cannot express, so a slice lowers to a raw pointer and a length.
  The Vego specification names that route for generated code, and every access stays bounds-checked.
- `src/main.rs` is the differential driver.
  It speaks the line protocol that `go1/revera/driver_host.go` defines.
- `src/host.rs` holds what the drivers and the fuzz entry point share: the embedded locale blob, its loader and the hex token decoder.
- `src/bench_main.rs` is the bench driver, and `src/fuzz.rs` with `src/fuzzcase_main.rs` is the fuzz entry point and its seed runner.
  The section below describes them.
- `fuzz/` is a cargo-fuzz crate over the same fuzz entry point.
- `tests/api.rs` covers the public API.

`Regex` is `Sync` even though `Arena` is not.
It never writes to its arena after `build`, and every search copies the header it walks.
Every allocation a search makes goes to an arena that the search owns and frees.

## Build and verify

```sh
cargo test
cargo build --release
cd ../go1 && go run ./cmd/crosscheck ../rust1/target/release/driver
cd ../go1 && go run ./cmd/probecheck ../rust1/target/release/probe
```

The build also produces `probe`, the runner for the `go1/probe` package.
That package covers the subset constructs the engine never uses.

One known limit.
A call that passes the same struct variable through two pointer arguments is valid Vego, but Rust rejects it at compile time with E0499.
The failure is loud, not silent.

Both profiles turn overflow checks off, so integer arithmetic wraps like Go.
The runtime uses `assert!` rather than `debug_assert!`, so an out-of-range index aborts in every profile.
That is the Go behavior the specification requires.

## Bench, fuzz and checked builds

The build also produces `bench` and `fuzzcase`.
Like the driver, they include the generated engine and the runtime directly and never link the public API.

`bench` speaks the bench protocol that `go1/revera/bench_host.go` defines.
A `B` line names one operation, compile, match or replace, with its iteration and repetition counts.
The answer gives the arena bytes and allocation requests of one operation, then the wall-clock nanoseconds of each repetition.
`Arena::stats` in `src/vg.rs` supplies the counts, so they cover engine-level requests only.
`go1/cmd/bench` drives every target with the same cases.

```sh
printf 'P\nB m match 100 3 0 28617c62292b 616261626162 2d\n' | target/release/bench
```

`src/fuzz.rs` is the fuzz entry point.
`fuzz_one` decodes one byte string into flags, a locale choice, a pattern, a replacement and a subject.
It then runs compile, exec, replace, the match iterator and the contract on them and ignores every result.
The input layout is shared with the other targets, so a corpus transfers between them.
`fuzzcase <packfile>` replays a pack of seed inputs and prints how many it ran.
A pack is a sequence of records, each a 4-byte little-endian length followed by that many bytes.
A crash is the only signal.

`fuzz/` is a cargo-fuzz crate over the same entry point.
It includes the runtime, the engine and `src/fuzz.rs` by path, so it needs no library crate.
With `cargo-fuzz` installed and the nightly toolchain:

```sh
cd fuzz && cargo +nightly fuzz run engine
```

Two builds keep every check on.
`cargo build` is the debug profile.
AddressSanitizer needs the nightly toolchain and an explicit target triple.
`asan-build.sh` reads the host triple from `rustc -vV` and runs that build:

```sh
sh asan-build.sh
```

The binaries land in `target/asan-bin/`.
