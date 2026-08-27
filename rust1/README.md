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

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`, are not on this surface.
A caller who needs them uses the hidden `revera::engine` module.

## Layout

- `src/lib.rs` is the public API.
  It embeds `data.bin` with `include_bytes!`.
- `src/engine.rs` is the generated engine.
  Regenerate it with:

  ```sh
  cd ../go1 && go run ./cmd/json2rust -o ../rust1/src/engine.rs revera.vego.json
  ```

- `src/vg.rs` is the runtime.
  It supplies the `Copy` `Slice<T>` and `Str` header types with Go slice semantics, the conversion and comparison helpers, and the `Arena` allocator.
  `Arena` is `!Sync`, so the compiler refuses to share one between threads.
  A Vego view can alias, which a Rust reference cannot express, so a slice lowers to a raw pointer and a length.
  The Vego specification names that route for generated code, and every access stays bounds-checked.
- `src/main.rs` is the differential driver.
  It speaks the line protocol that `go1/revera/driver_host.go` defines.
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
