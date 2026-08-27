# Target printers: progress and findings

Goal: from the work in go1/, write printers and implementations in
Zig, C++ and Rust, and verify they are correct.

## Plan

1. `go1/vegoc`: shared Go library. It loads revera.vego.json into a
   typed IR, infers the type of every expression, folds constant
   values, and computes per-function local usage and mutation.
2. Printers `go1/cmd/json2zig`, `json2cpp`, `json2rust` over that IR.
3. Hand-written minimal runtimes in `zig1/`, `cpp1/`, `rust1/`:
   arena allocation with a reset point, `Slice`/`Str` value types
   with Go slice-header copy semantics, embedded data.bin, and a
   differential driver executable.
4. `go1/cmd/crosscheck`: corpus generator and comparator. It mirrors
   the go1 differential tests, computes expected output with the Go
   engine in-process, and diffs each driver line by line.

## Design decisions

- All three targets lower `[]T` to a value struct {ptr, len, cap}
  and `string` to {ptr, len}. Assignment copies the header, so view
  and move semantics match Go exactly. The spec (section 9) blesses
  this pointer-and-length lowering for generated code.
- Memory comes from two arenas: a persistent one for locale data
  and a per-pattern one the driver resets at each compile. No other
  ownership tracking is needed.
- Wrap semantics: Zig uses `+% -% *%` and `@divTrunc`/`@rem`; C++
  builds with -fwrapv and casts every arithmetic result back to its
  Vego type to defeat integer promotion; Rust builds with
  overflow-checks off (as casts already truncate like Go).
- Two-result functions: Zig anonymous tuple, C++ emitted struct,
  Rust native tuple.
- Switch: native switch in Zig and C++ (no fallthrough in Vego),
  if-else chain on a bound temp in Rust (case values can be const
  expressions, which Rust patterns cannot hold).

## Driver protocol

Line-oriented, strings hex-encoded ("-" for empty), one output line
per input line:

- `P`                            -> `P 1`      reset to POSIX locale
- `L <namehex> <collhex>`        -> `L <ok>`   select locale
- `C <flags> <pathex>`           -> `C <code> <pos> <nsub>`
- `X <eflags> <subjhex>`         -> `X <code> <matched> so,eo ...`
- `R <limit> <eflags> <replhex> <subjhex>` -> `R <code> <pos> <outhex>`
- `I <limit> <eflags> <subjhex>` -> `I <code> <n> so,eo,...|...`
- `T <maxinput>`                 -> `T <hasSolver> <heap> <stack> <steps>`
- `O <lo> <hi>`                  -> `O <fnvhash of case maps>`

## Progress log

- Surveyed go1: spec, JSON stats, json2go, host file, tests.
  Notable: the JSON uses no range statements, no `^` complement,
  no `[]uint8(s)` conversion; 28 two-result functions; 6 array
  types; one global (an array of strings); all string literals are
  ASCII.
- Wrote go1/vegoc (loader, checker, analyses). Surprises the
  checker caught: `nil` appears as the zero-slice literal and one
  `firsts != nil` comparison needs real nil-ness (the runtimes give
  even zero-length allocations a non-null pointer, so nil tests the
  data pointer); `return f(...)` forwards a two-result call; the
  memo table compares `memoKey` structs with `==`.
- Zig target done and verified: json2zig printer, zig1/ runtime
  (vg.zig), driver, build.zig. Zig findings: unused params and
  never-mutated vars are hard errors (the checker's Used/Mutated
  flags drive `_ = x;` and var/const); locals must not shadow
  package-level names (vegoc renames); `u64` values need @intCast
  at index sites; anonymous tuple types are nominal, so two-result
  functions share named `Tup_*` types.
- Driver memory scheme (all targets): three arenas. Persistent
  holds locale data; pattern holds the compiled Regexp, reset at
  each C; scratch holds one command's workspace, reset per command.
  A single per-pattern arena thrashed: the solver-heavy patterns
  leak tens of MB per Exec, and 88 Execs on one pattern piled up
  gigabytes.
- Quick crosscheck corpus (26209 commands): Zig driver agrees with
  the Go engine on every line. Full corpus (86059) and an extended
  run with 10000 extra random patterns (156059 commands) also pass.
- C++ target done and verified (full corpus 86059/86059): json2cpp
  printer, cpp1/ runtime, driver, Makefile. C++ needed dependency
  sorting (structs by value containment, consts by reference),
  LL/ULL suffixes on every 64-bit literal (constexpr computes
  unsuffixed literals in int and rejects the overflow), per-struct
  `= default` equality only for all-scalar structs, and #embed
  (an Apple clang 21 C23 extension available in C++ mode).
- Rust target done and verified (full corpus 86059/86059):
  json2rust printer, rust1/ crate. Rust needed suffixed literals,
  raw-pointer element accessors (writes into fields of slice
  elements), labeled loops to keep continue-runs-post semantics,
  temp hoisting around &mut call arguments, use-site casts where
  an untyped Go constant adopts a second type, and a trailing
  unreachable!() for value functions that end in a loop.
- All three targets re-verified from clean regeneration by the
  parent session. Final joint run: crosscheck -extra 30 over Zig,
  C++ and Rust drivers together.

- A swival review over the finished targets found translator gaps
  outside the engine's constructs plus two latent engine-relevant
  bugs. All fixed:
  - Both runtimes now zero the spare capacity of grown buffers.
    Go zeroes allocations, and the engine reads slice extensions
    inside capacity; malloc had been handing out zero pages by
    luck. (Rust already used alloc_zeroed.)
  - C++ pins impure operands and arguments into ordered
    temporaries (Go is left-to-right; C++ is unsequenced), leaving
    pointer-typed arguments inline since they lower to references.
  - Signed division and remainder go through helpers everywhere:
    Go defines MinInt / -1 (Zig's @divTrunc and Rust's `/` trap).
  - `[]uint8(s)` conversion, `&^=` in C++, out-of-range MinInt
    literals, partial array literals in Rust and Zig, and array
    and string fields in compared structs all gained lowerings.
  - Range statements lower to a hidden counter over a
    once-evaluated operand in all three printers.
  - The drivers truncate both `O` endpoints like the reference,
    replacement errors carry their position, and the corpus sweeps
    eflags over replacement and iteration and matches
    case-sensitive patterns per locale.
- A follow-up review pass caught two more translator gaps, both
  fixed and probed: make with an unsigned length now casts at
  index positions in Zig (Rust already did; C++ converts
  implicitly and the runtime assert keeps Go's abort), and the C++
  index-pinning lambda returns by value when the base is an
  array-typed temporary, since a reference into it would dangle
  (addressable bases keep decltype(auto) so places stay
  assignable).
- go1/probe is a second Vego package covering exactly those
  engine-unused constructs. cmd/proberef prints the Go results;
  the target probe binaries print the same lines, and
  cmd/probecheck diffs them. All three targets match on all 24
  probe lines.

- A second swival round returned four findings, all fixed with
  probe coverage: package variables may not contain slices at any
  depth (vego2json and vegoc reject them; globals are static
  constant data); Rust accepts subslice views as assignment bases
  (a slice base is a value, not a place); Rust &^= uses the same
  pinned-place lowering as other compound assignments; and the
  C++ zero-length array view holds a static sentinel instead of
  arithmetic on a fabricated pointer. The probe suite covers 29
  result lines.

## Status: complete

The three printers, the three runtimes, the crosscheck harness
and the probe suite exist and pass. Every driver agrees with the
Go engine on every corpus line (191689 commands in the extended
run), and every probe binary agrees with the Go probe package.

## Follow-up: explicit memory contexts (2026-08-26)

The first design kept the three arenas as globals in each runtime,
behind a mode switch the driver flipped between calls. That made
the generated engines single-instance and thread-unsafe, and the
Rust runtime needed `static mut`.

The rework makes memory explicit and removes every global:

- vegoc computes a transitive per-function `Allocates` flag. The
  allocation sites are make, append, the two string conversions,
  and slice composite literals.
- Each printer gives every allocating function a synthetic first
  parameter named `mem` (reserved in the subset, enforced by the
  checker) and threads it through call sites. Zig passes a
  `std.mem.Allocator`, C++ a `vg::Arena&`, Rust a `&vg::Arena`
  whose block list sits in an UnsafeCell, so `Arena` is !Sync and
  cross-thread sharing rejects at compile time.
- The runtimes hold no state. The drivers own the three arenas as
  locals in main and pass the right one to each call; the mode
  API is gone. Spec section 9.1 records the scheme.

Functions that never allocate keep their plain signatures; the
analysis shows LocaleLoad, LocalePOSIX, MatchIterInit and the
contract queries allocate nothing. Crosscheck and probecheck pass
on all three targets after the rework.

## Follow-up: public APIs (2026-08-27)

The generated engine was the only surface each target offered. It
exports every internal function, takes a memory context, and
returns integer error codes. Each target now adds one hand-written
file above it, in the shape its own language expects: methods and
`error` in Go, a crate root with `Result` and iterators in Rust, a
module with an error set and optionals in Zig, and a pimpl header
with `std::optional` and exceptions in C++.

Three findings from the work:

- Only C++ needed a printer change. Go, Rust and Zig each have a
  second scope to put the generated code in (a host file, a
  private module, a separate file), while C++ has one namespace
  mechanism for both levels. `json2cpp` took a `-ns` flag, and the
  engine moved to `namespace revera::engine`.
- The wrapper cannot assume the engine answers its own question.
  `Exec` leaves `pmatch` untouched under `FlagNoSub` and still
  reports success, so an offset-returning wrapper has to refuse
  the call itself.
- Sharing one compiled expression between threads is sound in all
  four targets. Nothing writes the `Regexp` or its nodes after
  `Compile`; the wrappers copy the header per call and give each
  call its own arena. Rust states this with `unsafe impl Sync`,
  and every target's tests exercise it. Zig adds one condition:
  its searches allocate from the allocator the caller gave
  `compile`, so that allocator has to be thread safe. The other
  three reach a global allocator or the garbage collector.

The execution flags of `regexec()` stay off the four surfaces. The
iterators handle the piecewise-scan case that needs them, and the
generated engine stays reachable for the rest.
