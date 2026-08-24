# go1 rewrite: progress and findings

Goal: rewrite the go0/ ERE engine in a simplified Go subset under go1/.
Deliverables: the subset specification, the translated engine, and a
subset-to-JSON translator tool.

## Plan

1. Read every non-test source file in go0/ and list the Go features it uses.
2. Define the subset and write its specification in Markdown.
3. Translate the engine and the locale package into the subset.
4. Write the subset-to-JSON translator on top of go/ast.
5. Verify: the go1 package compiles, tests compare go1 against go0,
   and the translator accepts the whole go1 engine.

## Findings

### go0 inventory (non-test code)

| File        | Lines | Role                                    |
|-------------|-------|-----------------------------------------|
| regexp.go   | 268   | API surface, Compile/Exec               |
| flags.go    | 31    | flag constants                          |
| error.go    | 70    | error codes                             |
| syntax.go   | 403   | ERE parser                              |
| bracket.go  | 384   | bracket expression sets                 |
| program.go  | 446   | lowering to flat instruction program    |
| engine.go   | 545   | phase A lockstep matcher                |
| capture.go  | 416   | phase B capture solver                  |
| onepass.go  | 264   | one-pass capture walk                   |
| replace.go  | 190   | MatchAll/ReplaceAll/ReplaceAllFunc      |
| contract.go | 425   | resource contracts                      |
| oracle.go   | 427   | reference matcher (test support)        |
| locale/     | 844   | locale data access over data.bin        |

### Go features used by go0 that the subset cannot keep

- sync.Pool workspace pooling (regexp.go, engine.go, capture.go).
  The subset version allocates a fresh workspace per Exec call.
- Closures: walk() visitors, MatchAll/ReplaceAllFunc callbacks,
  buildScanFilter, tryCandidate in bestRep. All get restructured.
- Pointer trees: *node AST, *ptree parse trees, pointers in structs.
  Both trees become index arenas over flat slices.
- Maps: memo maps in capture.go, codeText in error.go. The memos become
  open-addressing hash tables written in the subset; the text map
  becomes a switch.
- error interface, defer, init(), go:embed, imports (utf8, slices,
  strings, cmp, math/bits, fmt). All removed or hand-coded.

### Design decisions

- Subset name: Vego. Spec lives in go1/VEGO-SPECIFICATION.md.
- One flat package: the locale package merges into the engine package
  with Locale-prefixed names. The data blob arrives as a string
  parameter; a *_host.go shim (outside the subset) embeds data.bin.
- No methods; free functions with an explicit receiver parameter.
- Fixed-width integers only; int stays allowed and means 64-bit.
  rune becomes int32. string stays: an immutable shareable byte view.
- Ownership model: []T fields own their buffer; subslices are transient
  views; copy() is overlap-safe. The spec states the exact rules.
- MatchAll/ReplaceAllFunc callbacks become a MatchIter iterator API;
  ReplaceAll builds on it. ReplaceAllFunc is host-runtime material.
- The oracle (reference matcher) stays out of the subset. Its shared
  helpers (decodeRune, decoded, atBOL/atEOL, structCmp, addCounters,
  fillMatches) move into the subset core. go1 tests compare against
  go0 directly, which subsumes the oracle.
- The in-place filter idiom (kept := q[:0]; append) gets rewritten
  with a write index, so translators never see self-aliasing appends.
- &slice[element] borrows are out; functions receive an index instead.

## Progress log

- Surveyed the repository. Read all go0 non-test sources and the
  locale package. Listed the feature gaps above.
- Chose the subset shape and the go1 layout:
  go1/VEGO-SPECIFICATION.md, go1/revera/ (subset package),
  go1/cmd/vego2json/ (translator), go1/revera.vego.json (output).
- Wrote VEGO-SPECIFICATION.md.
- Translated the whole engine: locale.go, utf8.go, error.go,
  flags.go, syntax.go, bracket.go, program.go, engine.go, hash.go,
  capture.go, onepass.go, match.go, regexp.go, replace.go,
  contract.go. Host shims live in revera_host.go (embed, MatchAll,
  ReplaceAllFunc, CompileWithContract, Open).
- Wrote differential tests against go0: locale operations, UTF-8
  decoding, random and fixed pattern corpora across flags, the cs
  multi-element locale, ReplaceAll, the iterator versus MatchAll,
  and a contract smoke test. All pass.
- Found and fixed: the solver arenas needed an entry cap. go0 eats
  20 GB on ((a*){250}){250} over 300 a's before its work limit
  reports ESpace; unlimited go1 arenas overflowed their int32
  offsets on the same input. go1 now trips solverArenaLimit and
  reports ESpace early, with no overflow.
- Interval stacking such as a{2}{3} is BadRpt in both engines (a
  duplication cannot follow a duplication), so the corpus keeps
  those as compile-error cases.
- Wrote cmd/vego2json: subset checking over go/ast plus go/types,
  and the JSON emitter. Running it on my own first translation
  found six real violations (a switch break, two three-value
  returns, a local const, a uint conversion); all fixed.
- Wrote cmd/json2go, the reference back-converter. The regenerated
  engine compiles and passes the whole differential suite, so the
  JSON provably carries the full program.
- Added the buffer-model store check to the tool: slice fields take
  only fresh buffers, moves, or self-truncations.
- Wrote README.md. gofmt and go vet are clean; revera.vego.json is
  checked in.

- A swival review reported six findings; all are fixed. High: a
  malformed locale blob could panic LocaleOpen through unvalidated
  cross-section offsets (localeValidate now checks every reference,
  and a corruption test covers it); the checker accepted pointer
  locals and pointer results (pointers are now parameter-only
  everywhere, and & is legal only as a direct call argument); the
  checker let a package-level value escape as a writable slice
  (globals can no longer have slice type, be sliced, or be passed
  as slices). Medium: rune-semantics string conversions are now
  rejected (only uint8 buffers convert), a malformed range
  statement no longer crashes the checker, and zero-argument
  append is rejected. The spec matches the tightened rules.

- A /simplify pass (four review agents) tightened the code without
  behavior changes: one globalBase mechanism in the checker, a
  scratch-seeded solver arena with bulk kid allocation and a shared
  kidPrepend, LocaleLoad/LocaleSelect so the host Open validates the
  embedded blob once, a shared indexOfByte, dead code removed
  (validUTF8, LocaleIsPOSIX, localeIndex), LocaleCollatingPrefix
  re-exported, the pooling-era runPhaseAWith wrapper merged away,
  ReplaceAllFunc built on MatchAll, and smaller tool and test
  cleanups. minMatchChars stays separate from computeLengths on
  purpose: the two saturation caps differ, and merging them would
  change the oversized-pattern fallback against go0.

## Status: complete

All three deliverables exist and are verified: the specification,
the Vego engine, and the JSON translator, plus the json2go
reference converter that proves the pipeline round-trips.
