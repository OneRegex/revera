# rust1: the revera engine in Rust

This directory is the Rust instantiation of the Vego engine from
`go1/`. `src/engine.rs` is generated from `go1/revera.vego.json`
by `go1/cmd/json2rust`; do not edit it. The hand-written files are
the public API and the minimal runtime the Vego spec requires each
target to supply.

## Using it

```rust
let re = revera::Regex::new("([a-z]+)([0-9]*)")?;
let caps = re.captures("__abc12__")?.expect("a match");
assert_eq!(&caps[1], "abc");
```

`src/lib.rs` is the crate root and the whole public surface.

- `Regex` compiles a pattern and searches. Search takes `&self`
  and keeps no state between calls, so a `Regex` is `Send` and
  `Sync`.
- `find` and `captures` return `Result<Option<_>>`: a subject that
  does not match is `None`, and only a real failure is `Err`.
  `Match` borrows the subject and has `as_str`, `range`, `start`
  and `end`.
- `find_iter` and `captures_iter` are lazy iterators over the
  non-overlapping matches.
- `Error` implements `std::error::Error`. `Error::kind` returns an
  `ErrorKind` variant and `Error::offset` the byte offset in the
  pattern.
- `RegexBuilder` carries the options:
  `RegexBuilder::new("ab+").case_insensitive(true).build()?`.
- `Locale::open("cs", "")` selects a CLDR locale for bracket
  expressions. The default is POSIX.
- `re.contract(max_input)` reports what one search can cost before
  it runs.

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`,
are not on this surface. A caller who needs them uses the hidden
`revera::engine` module directly.

## Layout

- `src/lib.rs` is the public API. It embeds `data.bin` with
  `include_bytes!`.
- `src/engine.rs` is the generated engine. Regenerate with:

  ```sh
  cd ../go1 && go run ./cmd/json2rust -o ../rust1/src/engine.rs revera.vego.json
  ```

- `src/vg.rs` is the runtime: Copy `Slice<T>` and `Str` header
  types with Go slice semantics, conversion and comparison
  helpers, and the `Arena` allocator. It holds no state. Every
  generated function that allocates takes an `&Arena` as its
  first parameter, `mem`. `Arena` is `!Sync`, so the compiler
  rejects sharing one between threads; each thread runs its own
  engine instance. Vego views can alias, which Rust references
  cannot express, so slices lower to raw pointer and length
  pairs, the route the Vego spec names for generated code. Every
  access stays bounds-checked.

  `Regex` is `Sync` all the same: it never writes to its arena
  after `build`, every search copies the header it walks, and
  every allocation a search makes goes to an arena that search
  owns and frees.
- `src/main.rs` is the differential driver. It speaks the line
  protocol defined in `go1/revera/driver_host.go`. It owns three
  arenas (persistent locale data, per-pattern, per-operation
  scratch) and passes the right one to each engine call.
- `tests/api.rs` covers the public API.

## Build and verify

```sh
cargo test
cargo build --release
cd ../go1 && go run ./cmd/crosscheck ../rust1/target/release/driver
cd ../go1 && go run ./cmd/probecheck ../rust1/target/release/probe
```

The build also produces `probe`, the runner for `go1/probe`: a
second Vego package covering the constructs the engine never
uses. One known limit: a call passing the same struct variable
through two pointer arguments is valid Vego but rejects at Rust
compile time (E0499), loudly rather than silently.

Both profiles turn overflow checks off, so integer arithmetic
wraps like Go. The runtime uses `assert!`, not `debug_assert!`,
so out-of-range indexing aborts in every profile, exactly the Go
behavior the spec requires.
