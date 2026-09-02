# Vego rewrite: progress and findings

This is a historical report; today the Vego engine lives in `go/`, the reference engine in `dev/internal/reference/`, the specification in `vego/SPECIFICATION.md`, and the checker and exporter in `vego/compiler` behind the `vegoc` command.

Goal: rewrite the reference ERE engine, now `dev/internal/reference/`, in a simplified Go subset, now `go/`.
Deliverables: the subset specification, the translated engine, and a subset-to-JSON translator tool.

## Findings

### Reference engine inventory (non-test code)

| File        | Role                                 |
|-------------|--------------------------------------|
| regexp.go   | API surface, Compile/Exec            |
| flags.go    | flag constants                       |
| error.go    | error codes                          |
| syntax.go   | ERE parser                           |
| bracket.go  | bracket expression sets              |
| program.go  | lowering to flat instruction program |
| engine.go   | phase A lockstep matcher             |
| capture.go  | phase B capture solver               |
| onepass.go  | one-pass capture walk                |
| replace.go  | MatchAll/ReplaceAll/ReplaceAllFunc   |
| contract.go | resource contracts                   |
| oracle.go   | reference matcher (test support)     |
| locale/     | locale data access over data.bin     |

### Go features used by the reference engine that the subset cannot keep

- sync.Pool workspace pooling (regexp.go, engine.go, capture.go).
  The subset version allocates a fresh workspace per Exec call.
- Closures: walk() visitors, MatchAll/ReplaceAllFunc callbacks, buildScanFilter, tryCandidate in bestRep.
  All get restructured.
- Pointer trees: *node AST, *ptree parse trees, pointers in structs.
  Both trees become index arenas over flat slices.
- Maps: memo maps in capture.go, codeText in error.go.
  The memos become open-addressing hash tables written in the subset.
  The text map becomes a switch.
- error interface, defer, init(), go:embed, imports (utf8, slices, strings, cmp, math/bits, fmt).
  All removed or hand-coded.

### Design decisions

- Subset name: Vego.
  Spec lives in `vego/SPECIFICATION.md`.
- One flat package: the locale package merges into the engine package with Locale-prefixed names.
  The data blob arrives as a string parameter.
  A *_host.go shim (outside the subset) embeds data.bin.
- No methods, so a free function takes an explicit receiver parameter.
- Fixed-width integers only, and int stays allowed as 64-bit.
  rune becomes int32.
  string stays, as an immutable shareable byte view.
- Ownership model: a []T field owns its buffer, a subslice is a transient view, and copy() is overlap-safe.
  The spec states the exact rules.
- MatchAll/ReplaceAllFunc callbacks become a MatchIter iterator API.
  ReplaceAll builds on it.
  ReplaceAllFunc is host-runtime material.
- The oracle (reference matcher) stays out of the subset.
  Its shared helpers (decodeRune, decoded, atBOL/atEOL, structCmp, addCounters, fillMatches) move into the subset core.
  The Go engine tests compare against the reference engine directly, which subsumes the oracle.
- The in-place filter idiom, `kept := q[:0]` followed by append, is rewritten with a write index.
  A translator therefore never sees a self-aliasing append.
- &slice[element] borrows are out.
  Functions receive an index instead.

## Progress log

- Surveyed the repository.
  Read all reference engine non-test sources and the locale package.
  Listed the feature gaps above.
- Chose the subset shape and the layout, today `vego/SPECIFICATION.md`, `go/` (subset package), `vego/compiler/export` behind `vegoc export` (translator), and `revera.vego.json` at the repository root (output).
- Wrote the specification.
- Translated the whole engine: locale.go, utf8.go, error.go, flags.go, syntax.go, bracket.go, program.go, engine.go, hash.go, capture.go, onepass.go, match.go, regexp.go, replace.go, contract.go.
  Host shims live in revera_host.go (embed, MatchAll, ReplaceAllFunc, CompileWithContract, Open).
- Wrote differential tests against the reference engine: locale operations, UTF-8 decoding, random and fixed pattern corpora across flags, the cs multi-element locale, ReplaceAll, the iterator versus MatchAll, and a contract smoke test.
  All pass.
- Found and fixed: the solver arenas needed an entry cap.
  The reference engine eats 20 GB on ((a*){250}){250} over 300 a's before its work limit reports ESpace.
  Unlimited arenas in the Go engine overflowed their int32 offsets on the same input.
  The Go engine now trips solverArenaLimit and reports ESpace early, with no overflow.
- Interval stacking such as a{2}{3} is BadRpt in both engines (a duplication cannot follow a duplication), so the corpus keeps those as compile-error cases.
- Wrote the checker and exporter, today `vegoc check` and `vegoc export`: subset checking over go/ast plus go/types, and the JSON emitter.
  Running it on my own first translation found six real violations (a switch break, two three-value returns, a local const, a uint conversion).
  All fixed.
- Wrote the Go printer, today `vegoc emit go`, the reference back-converter.
  The regenerated engine compiles and passes the whole differential suite, so the JSON provably carries the full program.
- Added the buffer-model store check to the tool: slice fields take only fresh buffers, moves, or self-truncations.
- Wrote README.md.
  gofmt and go vet are clean.
  Revera.vego.json is checked in.

- A swival review reported six findings.
  All are fixed.
  High: a malformed locale blob could panic LocaleOpen through unvalidated cross-section offsets.
  localeValidate now checks every reference, and a corruption test covers it.
  The checker accepted pointer locals and pointer results, so pointers are now parameter-only everywhere, and `&` is legal only as a direct call argument.
  The checker let a package-level value escape as a writable slice, so a global can no longer have slice type, be sliced, or be passed as a slice.
  Medium: rune-semantics string conversions are now rejected (only uint8 buffers convert), a malformed range statement no longer crashes the checker, and zero-argument append is rejected.
  The spec matches the tightened rules.

- A /simplify pass with four review agents tightened the code, with no change in behavior.
  The checker gained one globalBase mechanism.
  The solver arena became scratch-seeded, with bulk kid allocation and a shared kidPrepend.
  LocaleLoad and LocaleSelect let the host Open check the embedded blob once.
  indexOfByte became shared, and the dead validUTF8, LocaleIsPOSIX and localeIndex went.
  LocaleCollatingPrefix came back as an export, the pooling-era runPhaseAWith wrapper merged away, and ReplaceAllFunc now builds on MatchAll.
  Smaller tool and test cleanups came with them.
  minMatchChars stays separate from computeLengths on purpose, because the two saturation caps differ.
  A merge would change the oversized-pattern fallback against the reference engine.

- An audit found one crash path in the solver.
  Once an arena limit trips, every allocator hands out offset zero, so a record could name itself as its own child.
  A counter comparison over such a record never returned, and each target would overflow its stack.
  A limit scan in a scratch copy showed the cycle on `x*?(a*|(a*){250})`.
  cmpCand now stops once the solver has failed, and capture_test.go pins that.
  The same audit fixed the printers for constructs the engine never uses.
  A constant expression prints as one folded literal in C, C++, and Rust, so an intermediate value no longer truncates to a leaf type.
  A multi-element append pins its elements when a later one could read the appended buffer.
  Zig casts a comptime-narrowed @min or @max back to the Go type where a wrapping operator consumes it.
  The checker rejects a two-value assignment whose later place reads an earlier target, and it no longer folds a variable shift.
  The C, C++, and Zig drivers read lines of any length, and the Zig driver no longer caps its iteration rows.

## Status: complete

All three deliverables exist and are verified: the specification, the Vego engine, and the JSON translator, plus the `vegoc emit go` reference converter that proves the pipeline round-trips.
