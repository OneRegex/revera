# Target printers: progress and findings

This is a historical report; today the printers live under `vego/compiler/printer` behind `vegoc emit`, and the targets in `rust/`, `zig/`, `native/cpp/` and `native/c/`.

Goal: from the work on the Vego engine, now in `go/`, write printers and implementations in Zig, C++ and Rust, and verify they are correct.

## Design decisions

- All three targets lower `[]T` to a value struct {ptr, len, cap} and `string` to {ptr, len}.
  Assignment copies the header, so view and move semantics match Go exactly.
  The spec (section 9) blesses this pointer-and-length lowering for generated code.
- Memory comes from two arenas: a persistent one for locale data and a per-pattern one the driver resets at each compile.
  No other ownership tracking is needed.
- Wrap semantics differ per target.
  Zig uses `+% -% *%` with `@divTrunc` and `@rem`.
  C++ builds with -fwrapv, and casts every arithmetic result back to its Vego type to defeat integer promotion.
  Rust builds with overflow-checks off, because an `as` cast already truncates like Go.
- Two-result functions: Zig anonymous tuple, C++ emitted struct, Rust native tuple.
- Switch: native switch in Zig and C++ (no fallthrough in Vego), if-else chain on a bound temp in Rust (case values can be const expressions, which Rust patterns cannot hold).

## Driver protocol

Line-oriented, strings hex-encoded ("-" for empty), one output line per input line:

- `P`                            -> `P 1`      reset to POSIX locale
- `L <namehex> <collhex>`        -> `L <ok>`   select locale
- `C <flags> <pathex>`           -> `C <code> <pos> <nsub>`
- `X <eflags> <subjhex>`         -> `X <code> <matched> so,eo ...`
- `R <limit> <eflags> <replhex> <subjhex>` -> `R <code> <pos> <outhex>`
- `I <limit> <eflags> <subjhex>` -> `I <code> <n> so,eo,...|...`
- `T <maxinput>`                 -> `T <hasSolver> <heap> <stack> <steps>`
- `O <lo> <hi>`                  -> `O <fnvhash of case maps>`

## Progress log

- Surveyed the Vego engine: spec, JSON stats, the Go printer, host file, tests.
  Notable findings: the JSON uses no range statement, no `^` complement, and no `[]uint8(s)` conversion.
  It holds 28 two-result functions, 6 array types, and one global, which is an array of strings.
  All string literals are ASCII.
- Wrote the shared front end, today `vego/compiler` (loader, checker, analyses).
  Surprises the checker caught: `nil` appears as the zero-slice literal, and one `firsts != nil` comparison needs real nil-ness.
  The runtimes give even a zero-length allocation a non-null pointer, so a nil test reads the data pointer.
  `return f(...)` forwards a two-result call.
  The memo table compares `memoKey` structs with `==`.
- Zig target done and verified: the Zig printer, today `vegoc emit zig`, the `zig/` runtime (vg.zig), driver, build.zig.
  Zig findings: an unused parameter and a never-mutated var are hard errors.
  The Used and Mutated flags of the checker drive `_ = x;` and the var or const choice.
  A local must not shadow a package-level name, which vegoc renames.
  A `u64` value needs @intCast at an index site.
  Anonymous tuple types are nominal, so two-result functions share named `Tup_*` types.
- Driver memory scheme (all targets): three arenas.
  Persistent holds the locale data.
  Pattern holds the compiled Regexp, and resets at each C command.
  Scratch holds the workspace of one command, and resets per command.
  A single per-pattern arena thrashed: the solver-heavy patterns leak tens of MB per Exec, and 88 Execs on one pattern piled up gigabytes.
- Quick crosscheck corpus (26209 commands): Zig driver agrees with the Go engine on every line.
  Full corpus (86059) and an extended run with 10000 extra random patterns (156059 commands) also pass.
- C++ target done and verified (full corpus 86059/86059): the C++ printer, today `vegoc emit cpp`, the `native/cpp/` runtime, driver, Makefile.
  C++ needed dependency sorting, of structs by value containment and consts by reference.
  It needed an LL or ULL suffix on every 64-bit literal, because constexpr computes an unsuffixed literal in int and rejects the overflow.
  It needed per-struct `= default` equality for all-scalar structs only.
  It also needed #embed, an Apple clang 21 C23 extension that works in C++ mode.
- Rust target done and verified (full corpus 86059/86059): the Rust printer, today `vegoc emit rust`, the `rust/` crate.
  Rust needed suffixed literals, and raw-pointer element accessors for writes into fields of slice elements.
  It needed labeled loops to keep the continue-runs-post semantics, and temp hoisting around &mut call arguments.
  It needed a use-site cast where an untyped Go constant adopts a second type.
  It also needed a trailing unreachable!() for a value function that ends in a loop.
- All three targets re-verified from clean regeneration by the parent session.
  Final joint run: crosscheck -extra 30 over Zig, C++ and Rust drivers together.

- A swival review over the finished targets found translator gaps outside the engine's constructs plus two latent engine-relevant bugs.
  All fixed:
  - Both runtimes now zero the spare capacity of grown buffers.
    Go zeroes allocations, and the engine reads slice extensions inside capacity; malloc had been handing out zero pages by luck.
    (Rust already used alloc_zeroed.)
  - C++ pins impure operands and arguments into ordered temporaries, because Go is left-to-right and C++ is unsequenced.
    A pointer-typed argument stays inline, because it lowers to a reference.
  - Signed division and remainder go through helpers everywhere: Go defines MinInt / -1 (Zig's @divTrunc and Rust's `/` trap).
  - `[]uint8(s)` conversion, `&^=` in C++, out-of-range MinInt literals, partial array literals in Rust and Zig, and array and string fields in compared structs all gained lowerings.
  - Range statements lower to a hidden counter over a once-evaluated operand in all three printers.
  - The drivers truncate both `O` endpoints like the reference, replacement errors carry their position, and the corpus sweeps eflags over replacement and iteration and matches case-sensitive patterns per locale.
- A follow-up review pass caught two more translator gaps, and both are fixed and probed.
  make with an unsigned length now casts at index positions in Zig.
  Rust already did that, and C++ converts implicitly while its runtime assert keeps the abort of Go.
  The C++ index-pinning lambda also returns by value when the base is an array-typed temporary, because a reference into it would dangle.
  An addressable base keeps decltype(auto), so a place stays assignable.
- `vego/probe` is a second Vego package covering exactly those engine-unused constructs.
  `dev/internal/conformance/proberef` prints the Go results.
  The target probe binaries print the same lines, and `dev/internal/conformance/probecheck` diffs them.
  All three targets match on all 24 probe lines.

- A second swival round returned four findings, and probe coverage now backs every fix.
  A package variable may not contain a slice at any depth, so the checker, today `vegoc check`, rejects one, because a global is static constant data.
  Rust accepts a subslice view as an assignment base, because a slice base is a value and not a place.
  Rust &^= uses the same pinned-place lowering as the other compound assignments.
  The C++ zero-length array view holds a static sentinel, instead of arithmetic on a fabricated pointer.
  The probe suite covers 29 result lines.

## Status: complete

The three printers, the three runtimes, the crosscheck harness and the probe suite exist and pass.
Every driver agrees with the Go engine on every corpus line (191689 commands in the extended run), and every probe binary agrees with the Go probe package.

## Follow-up: explicit memory contexts (2026-08-26)

The first design kept the three arenas as globals in each runtime, behind a mode switch the driver flipped between calls.
That made the generated engines single-instance and thread-unsafe, and the Rust runtime needed `static mut`.

The rework makes memory explicit and removes every global:

- vegoc computes a transitive per-function `Allocates` flag.
  The allocation sites are make, append, the two string conversions, and slice composite literals.
- Each printer gives every allocating function a synthetic first parameter named `mem` (reserved in the subset, enforced by the checker) and threads it through call sites.
  Zig passes a `std.mem.Allocator`, C++ a `vg::Arena&`, Rust a `&vg::Arena` whose block list sits in an UnsafeCell, so `Arena` is !Sync and cross-thread sharing rejects at compile time.
- The runtimes hold no state.
  The drivers own the three arenas as locals in main and pass the right one to each call.
  The mode API is gone.
  Spec section 9.1 records the scheme.

Functions that never allocate keep their plain signatures.
The analysis shows LocaleLoad, LocalePOSIX, MatchIterInit and the contract queries allocate nothing.
Crosscheck and probecheck pass on all three targets after the rework.

## Follow-up: public APIs (2026-08-27)

The generated engine was the only surface each target offered.
It exports every internal function, takes a memory context, and returns integer error codes.
Each target now adds one hand-written file above it, in the shape its own language expects.
Go gets methods and `error`.
Rust gets a crate root with `Result` and iterators.
Zig gets a module with an error set and optionals.
C++ gets a pimpl header with `std::optional` and exceptions.

Three findings from the work:

- Only C++ needed a printer change.
  Go, Rust and Zig each have a second scope for the generated code: a host file, a private module, or a separate file.
  C++ has one namespace mechanism for both levels.
  The C++ printer, today `vegoc emit cpp`, took a `-namespace` flag, and the engine moved to `namespace revera::engine`.
- The wrapper cannot assume the engine answers its own question.
  `Exec` leaves `pmatch` untouched under `FlagNoSub` and still reports success, so an offset-returning wrapper has to refuse the call itself.
- Sharing one compiled expression between threads is sound in all four targets.
  Nothing writes the `Regexp` or its nodes after `Compile`.
  The wrappers copy the header per call and give each call its own arena.
  Rust states this with `unsafe impl Sync`, and every target's tests exercise it.
  Zig adds one condition: its searches allocate from the allocator the caller gave `compile`, so that allocator has to be thread safe.
  The other three reach a global allocator or the garbage collector.

The execution flags of `regexec()` stay off the four surfaces.
The iterators handle the piecewise-scan case that needs them, and the generated engine stays reachable for the rest.
