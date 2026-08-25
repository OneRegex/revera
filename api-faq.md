# API FAQ

Notes on standard-library behavior that differed from first expectations.

## Zig (0.17.0-dev)

### Reified integer types: @Type and std.meta.Int are gone

- Context: building an unsigned type of a given width inside a
  generic Go-style integer conversion helper.
- Expectation: `std.meta.Int(.unsigned, bits)` or
  `@Type(.{ .int = ... })`, as in every published example.
- Reality: both are removed. The replacement is a dedicated
  builtin: `@Int(.unsigned, bits)`.

### @typeInfo struct field access

- Context: iterating struct declarations to force full semantic
  analysis, and comparing struct values field by field.
- Expectation: `@typeInfo(T).@"struct".decls` and `.fields` with
  `field.name`.
- Reality: the Struct info now exposes parallel arrays:
  `decl_names`, `field_names`, `field_types`. `std.testing` also
  lost `refAllDeclsRecursive`; loop over `decl_names` with
  `@field` instead.

### Entry point and I/O

- Context: a CLI driver reading stdin line by line.
- Reality: `pub fn main(init: std.process.Init)` receives the Io
  instance, arena, and args. Readers and writers wrap files:
  `var r: std.Io.File.Reader = .init(.stdin(), init.io, &buf);`
  then `r.interface.takeDelimiter('\n')` returns `?[]u8`, null at
  EOF. `std.heap.ArenaAllocator` still exists unchanged.

### Build system optimize option

- Context: building a release binary with `zig build`.
- Expectation: `zig build -Doptimize=ReleaseSafe`.
- Reality: the flag is now `-Drelease`, and the build script's
  `standardOptimizeOption(.{ .preferred_optimize_mode = ... })`
  picks which release mode that flag selects.

## C++ (Apple clang 21)

### #embed in C++ mode

- Context: embedding the locale blob into the C++ driver.
- Expectation: #embed is C23 only, so C++ would need a generated
  byte-array source.
- Reality: Apple clang 21 accepts #embed in C++ mode as an
  extension. It warns under -Wc23-extensions; the build silences
  that one warning.

### constexpr shifts compute in int

- Context: emitting `1 << 31` and large constant products from the
  mechanical translator.
- Reality: an unsuffixed literal is `int`, and constexpr
  evaluation rejects the overflow instead of wrapping. The printer
  suffixes every 64-bit-typed literal with LL/ULL, not only those
  above INT32_MAX.

### constexpr forbids signed negation overflow

- Context: emitting Go's `-9223372036854775808` and unary minus on
  possibly-minimal values.
- Reality: the literal has no signed spelling, and negating the
  minimum overflows, which constexpr rejects even under -fwrapv.
  The printer converts the literal from unsigned
  (`int64_t(...ULL)`, modular since C++20) and computes every
  integer negation as `T(0ULL - uint64_t(x))`.

### Warnings on generated code

- Context: the printer parenthesizes every binary expression.
- Reality: clang's -Wparentheses-equality fires on `if ((a == b))`.
  The build disables that warning rather than special-casing the
  printer.

## Rust (1.98)

### Assignment evaluates the right side first

- Context: preserving Go's left-to-right evaluation in generated
  assignments whose left side contains a call.
- Reality: Rust evaluates the value expression before the place
  expression. The printer pins the place first (through a raw
  pointer accessor) at the three sites where the left side calls.

### Borrows in generated call arguments

- Context: `compileScan(&mut re, re.root)`-shaped calls, legal in
  Go, reject in Rust with E0502.
- Resolution: the printer hoists the non-borrow arguments into
  temporaries in Go's left-to-right order and takes the `&mut`
  borrows last. `&raw mut` (stable) borrows array storage for
  slice views without creating references.

### One declared type per constant

- Context: Go untyped constants adopt a type at each use site; the
  engine uses one constant as both i64 and u32.
- Reality: a Rust `const` has one type. The printer emits a
  use-site cast whenever the checker's per-node type differs from
  the constant's declared type. Zig never shows the problem
  because comptime_int coerces.

## Go

### regexp.CompilePOSIX newline handling

- Context: using Go's POSIX mode as a differential oracle for
  leftmost-longest whole-match selection.
- Expectation: `CompilePOSIX` implements POSIX matching, so dot matches
  newline and `^`/`$` bind to the subject boundaries.
- Reality: the POSIX flag set keeps RE2's newline conventions. Dot and
  negated classes exclude newline, and anchors match at every line
  boundary. `(?s)` cannot fix it: flag groups are not POSIX syntax and
  fail to compile.
- Resolution: the comparison uses newline-free subjects and avoids
  negated classes, so both conventions coincide.

### testing.AllocsPerRun under the race detector

- Context: asserting a zero-allocation steady state for the match path.
- Reality: race instrumentation adds allocations, so the assertion is
  only valid without `-race`. The test skips itself via a build-tagged
  `raceEnabled` flag.
