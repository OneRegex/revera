# The LEAN4 model of Vego

This directory holds the Lean 4 formalization that the Vego
specification promises: a formal semantics for the subset, applied
to the exact JSON artifacts that the Go, Rust, Zig and C++ printers
consume. The theorems in `Vego/Theorems.lean` are machine checked.

## What is proved

All four theorems are about the embedded copies of
`go1/probe.vego.json` and `go1/revera.vego.json`, byte for byte.

1. `probe_wellformed`, `revera_wellformed`. Both programs decode
   from JSON and elaborate into a fully typed core. Elaboration
   replays what the Go compiler and vego2json guarantee: every name
   resolves, every operator gets one width, untyped constants fold
   exactly and fit their contexts, composite literals are complete,
   and control flow is well shaped. Success is a well-formedness
   proof for the shipped artifacts.

2. `probe_agrees`. Under the formal semantics, the probe program
   reproduces the 29 report lines of the Go original
   (`lean/data/probe.expected`, from `cmd/proberef`). The probe
   matrix pins the semantic corners of the language: division
   overflow, wrapping at every width, evaluation order, spare
   capacity zeroing, nil-ness, range copies, struct equality with
   string arrays, and writes through subslice views.

3. `revera_corpus_agrees`. Under the formal semantics, the revera
   engine answers the embedded crosscheck corpus
   (`lean/data/corpus.tsv`, commands with the Go engine's output)
   exactly like the Go reference, through the same driver protocol
   as the Zig, C++ and Rust targets: compile, execute, replace,
   iterate, contracts, locale selection and case digests. The runs
   hit no trap: no out-of-range index or slice, no division by
   zero, no impossible shift, no ill-typed step, anywhere in the
   millions of interpreted operations these commands execute.

   The corpus covers all 86689 commands with no exclusions.
   Checking the theorem replays every one of them through the
   interpreter, which takes tens of minutes of native evaluation;
   the built module caches the proof. The `vegocheck` executable
   replays any corpus dump from disk with the same code path, and
   `runCorpusFuel` also accepts a fuel bound. Fuel caps the
   recursion depth of each engine call, not a command's aggregate
   work, so it makes a replay deterministic under a bound rather
   than fast.

## What this means for the pipeline

The generated Rust, Zig and C++ engines are produced from the same
JSON the theorems talk about, and `cmd/crosscheck` verifies all
three against the Go engine on the same corpus. The Lean semantics
therefore anchors the whole chain: JSON → formal semantics →
reference outputs on one side, JSON → printer → target engine →
same outputs on the other.

## Structure

```
Vego/Ast.lean       raw syntax tree, the image of the JSON grammar
Vego/Decode.lean    total JSON decoder
Vego/Data.lean      the two embedded .vego.json artifacts
Vego/Core.lean      typed core: resolved names, widths, zero fills
Vego/Elab.lean      elaborator (checker): raw tree to typed core
Vego/Interp.lean    operational semantics: heap, traps, fuel
Vego/Machine.lean   host API: init, call, allocate, read back
Vego/Probe.lean     probe harness (the Lean probe_host)
Vego/Driver.lean    driver protocol session (the Lean driver_host)
Vego/Theorems.lean  the machine-checked statements
Main.lean           vegocheck: replay any corpus dump from disk
data/               probe.expected, corpus.tsv, localedata.hex
```

## The semantics in one paragraph

Memory is a heap of cells. Each local variable lives in a cell;
each buffer from make, append, a slice literal or a byte
conversion lives in its own cell. A slice value is a header (cell,
path, offset, length, capacity), so views alias exactly as in Go,
including views of local arrays. Values carry canonical integers;
every operation wraps at the width the elaborator assigned.
Abnormal terminations are traps, and the interpreter is total:
it recurses on fuel and reports exhaustion as a trap. Fuel bounds
the depth of one engine call, not its aggregate work. Growth
follows the portable runtime contract of the targets:
max(2*cap, 8, need) with a zeroed spare region. The Go original
grows with Go's own append, so capacities after growth differ;
the specification declares them target defined and keeps them
out of observable output.

## Trust base

Proofs use `native_decide`, so they rest on the Lean kernel plus
the Lean compiler (`Lean.ofReduceBool`). The reference outputs are
produced by the Go engine; the theorems state agreement with those
recorded outputs, not POSIX correctness in the abstract. The
elaborator does not re-check the buffer-ownership clauses that
vego2json leaves to review (moves and rule 9); the semantics does
not need them, because aliasing is modeled directly.

## Regenerating the data

```
cd go1
go run ./cmd/proberef > ../lean/data/probe.expected
go run ./cmd/crosscheck -dumpexpected ../lean/data/corpus.tsv
xxd -p revera/data.bin | tr -d '\n' > ../lean/data/localedata.hex
cd ../lean && lake build && .lake/build/bin/vegocheck data/corpus.tsv
```

`lake build` checks the theorems; expect the corpus theorem to
evaluate for tens of minutes (a few pathological solver commands
account for most of it). The toolchain is pinned in
`lean-toolchain`.
