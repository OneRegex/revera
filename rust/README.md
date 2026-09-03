# Revera for Rust

Revera is a POSIX.1-2024 extended regular expression engine.
This directory is the Rust instantiation and the source of the `revera` crate for crates.io.

The same engine exists in Go, Rust, Zig, C, C++ and TypeScript, generated from one Vego source and exercised by one cross-language conformance suite.
In addition, the Lean development gives Vego machine-checked semantics, and the repository's Lean README states its exact proof coverage.

The engine speaks the POSIX ERE language: leftmost-longest matching, no backreferences, no Perl escapes.
Therefore, it is the engine to reach for when a pattern must mean the same thing as it does in `regcomp()` and `regexec()`.
For package discovery, the crate keywords are regex, posix, ere and regular-expressions.

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
  `Error::kind` returns an `ErrorKind` variant, and `Error::offset` returns an optional byte offset.
  Compilation offsets refer to the pattern, while replacement syntax offsets refer to the replacement text.
- `RegexBuilder` carries the options: `RegexBuilder::new("ab+").case_insensitive(true).build()?`.
- `Locale::open("cs", "")` selects a CLDR locale for bracket expressions.
  The default is POSIX.
- `re.contract(max_input)` reports what one search can cost, before it runs.

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`, are not exposed by this crate.

## Layout

The directory is a Cargo workspace with two packages.
The root package is the library, and it is the only one that ships.
The second package, `tools`, holds the binaries that the conformance kit and the bench harness drive.

```
|-------------------------------|-------------------------------------------------|
| Path                            | Role                                              |
| ------------------------------- | ------------------------------------------------- |
| src/lib.rs                      | The public API. It embeds data.bin.               |
| src/engine.rs                   | The generated engine. Do not edit it.             |
| src/probe_engine.rs             | The generated probe. Do not edit it.              |
| src/vg.rs                       | The runtime the Vego specification asks for.      |
| src/data.bin                    | The CLDR locale blob.                             |
| tests/api.rs                    | Tests of the public API.                          |
| tools/src/main.rs               | The differential driver.                          |
| tools/src/probe_main.rs         | The probe runner.                                 |
| tools/src/bench_main.rs         | The bench driver.                                 |
| tools/src/fuzz.rs               | The fuzz entry point.                             |
| tools/src/fuzzcase_main.rs      | The seed pack runner.                             |
| tools/src/host.rs               | What the binaries share: blob, loader, hex.       |
| fuzz/                           | A cargo-fuzz crate over the same entry point.     |
| backend.json                    | How the conformance kit builds and runs this.     |
| asan-build.sh                   | The AddressSanitizer build of the tools.          |
| ------------------------------- | ------------------------------------------------- |
```

`src/engine.rs` and `src/probe_engine.rs` come from `revera.vego.json` at the repository root.
The printer is `vegoc emit rust`, the package `vego/compiler/printer/rust`.

Regenerate them from the repository root:

```sh
make generate GENERATION_TARGETS=rust
```

The same thing runs as `cd ../dev && go run ./cmd/generate -target rust`.
The output is byte-exact and is not `rustfmt` clean by design, so never format the generated files.

The `driver`, `bench` and `fuzzcase` binaries include `src/engine.rs` and `src/vg.rs` by path with `#[path]` attributes.
The `probe` binary includes `src/probe_engine.rs` and `src/vg.rs` the same way.

Because those binaries never link the library crate, a driver failure points at the engine or the runtime and not at the API layer.
`tools/src/host.rs` supplies the embedded locale data and protocol helpers to the engine-based tools.

`src/vg.rs` is the runtime.
It supplies the `Copy` `Slice<T>` and `Str` header types with Go slice semantics, the conversion and comparison helpers, and the `Arena` allocator.
The generated modules forbid unsafe code.
Raw-pointer dereferences and arithmetic stay in this hand-written runtime, behind bounds-checked value and slot operations.

`Arena` is `!Sync`, so the compiler refuses to share one between threads.
A Vego view can alias, which a Rust reference cannot express, so a slice lowers to a raw pointer and a length.
The Vego specification explicitly permits this lowering, and every access stays bounds-checked.

`Regex` is `Sync` even though `Arena` is not.
It never writes to its arena after `build`, and every search copies the header it walks.
Moreover, every allocation a search makes goes to an arena that the search owns and frees.
Moreover, every allocation that a search makes goes to an arena that the search owns and frees.

## Build and verify

```sh
cargo test --workspace
cargo build --release --workspace
(cd ../dev && go run ./internal/conformance/crosscheck ../rust/target/release/driver)
(cd ../dev && go run ./internal/conformance/probecheck ../rust/target/release/probe)
```

`driver` speaks the line protocol that `dev/internal/protocol/driver.go` defines, and `crosscheck` runs the corpus through it.
`probe` prints the lines of the `vego/probe` package, which covers the subset constructs the engine never uses.
For comparison, `dev/internal/conformance/proberef` prints the reference lines, and `probecheck` compares the two outputs.

The one-command check is the conformance kit:

```sh
(cd ../dev && go run ./cmd/conform -backend ../rust)
```

It reads `backend.json`, builds the release and checked variants, and runs the probe and fuzz seeds against each.
Specifically, the release build runs the full corpus and the random stress rounds, while checked builds run the smaller checked corpus and omit stress.

One known limit remains.
A call that passes the same struct variable through two pointer arguments is valid Vego, but Rust rejects it at compile time with E0499.
In that case, the failure is loud, not silent.

Both profiles turn overflow checks off, so integer arithmetic wraps like Go.
Because the runtime uses `assert!` rather than `debug_assert!`, an out-of-range index aborts in every profile.
This matches the Go behavior that the specification requires.

Cargo reads profiles from the workspace root, so the two profiles live in the root `Cargo.toml` and apply to the tools as well.

## Bench, fuzz and checked builds

The release build also produces `bench` and `fuzzcase` under `target/release/`.

`bench` speaks the bench protocol that `dev/internal/protocol/bench.go` defines.
A `B` line names one operation, compile, match or replace, with its iteration and repetition counts.

The answer gives the arena bytes and allocation requests of one operation, then the wall-clock nanoseconds of each repetition.
`Arena::stats` in `src/vg.rs` supplies the counts, so they cover engine-level requests only.
Finally, `dev/cmd/bench` drives every target with the same cases.

```sh
printf 'P\nB m match 100 3 0 28617c62292b 616261626162 2d\n' | target/release/bench
```

`tools/src/fuzz.rs` is the fuzz entry point.
`fuzz_one` decodes one byte string into flags, a locale choice, a pattern, a replacement and a subject.

It then runs compile, exec, replace, the match iterator and the contract on them, and it ignores every result.
The input layout is `dev/internal/protocol/fuzz.go`, shared with the other targets, so a corpus transfers between them.

`fuzzcase <packfile>` replays a pack of seed inputs and prints how many it ran.
A pack is a sequence of records, each a 4-byte little-endian length followed by that many bytes.
Therefore, a crash is the only signal.

`fuzz/` is a cargo-fuzz crate over the same entry point.
It includes the runtime, the engine, `tools/src/host.rs` and `tools/src/fuzz.rs` by path, so it needs no library crate.

It declares its own workspace, and the root manifest excludes it.
With `cargo-fuzz` installed and the nightly toolchain:

```sh
cd fuzz && cargo +nightly fuzz run engine
```

Two builds keep every check on.
`cargo build --workspace` is the debug profile.

AddressSanitizer needs the nightly toolchain and an explicit target triple.
`asan-build.sh` reads the host triple from `rustc -vV` and runs that build for the whole workspace:

```sh
sh asan-build.sh
```

The binaries land in `target/asan-bin/`.

## What ships

`Cargo.toml` declares version `0.1.0` and the MIT license.
`cargo package --list` includes `src/lib.rs`, `src/engine.rs`, `src/vg.rs`, `src/data.bin`, `tests/api.rs` and this README.
It also includes the Cargo package metadata and `LICENSE`.
The license for the embedded data ships as `LICENSES/Unicode-3.0.txt`.

`LICENSE` and `LICENSES/` are copies of the repository root files that `make licenses` keeps current.
Accordingly, the repository's release staging command refuses missing or stale copies.

By contrast, the tools, the fuzz crate, `backend.json` and `asan-build.sh` stay in the repository.
