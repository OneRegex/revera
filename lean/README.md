# The LEAN4 model of Vego

This directory holds the Lean 4 formalization that the Vego
specification promises: a formal semantics for the subset, applied
to the exact JSON artifacts that the Go, Rust, Zig and C++ printers
consume. The theorems in `Vego/Theorems.lean` are machine checked.

## What is proved

The theorems of `Vego/Theorems.lean` are about the embedded copies
of `go1/probe.vego.json` and `go1/revera.vego.json`, byte for
byte, and are checked by native evaluation. The theorems of
`Vego/CostLemmas.lean` and `Vego/MeterSound.lean` quantify over
all inputs and are proved by induction, with no native evaluation.

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

3. `revera_corpus_agrees_within_contract`. Under the formal
   semantics, the revera engine answers the embedded crosscheck
   corpus (`lean/data/corpus.tsv`, commands with the Go engine's
   output) exactly like the Go reference, through the same driver
   protocol as the Zig, C++ and Rust targets: compile, execute,
   replace, iterate, contracts, locale selection and case digests.
   The runs hit no trap: no out-of-range index or slice, no
   division by zero, no impossible shift, no ill-typed step,
   anywhere in the millions of interpreted operations these
   commands execute.

   The same theorem also proves the resource contracts sound over
   the executions it measures. The interpreter meters each Exec
   the session calls: the bytes of every buffer allocation (make,
   append growth, slice literals, and the string conversions, at
   the fixed 64-bit layout the targets share), the deepest call
   chain, and one abstract step per loop iteration and per call.
   Before each one the session computes the pattern's contract for
   the subject length with the engine's own interpreted
   ContractFor, and a measurement above ContractHeapBytes,
   ContractStackBytes (at the contract's 256-byte frame estimate),
   or ContractSteps is a hard fault.

   That covers the X commands, which are 76112 of the corpus. It
   does not cover R and I: ReplaceAll and MatchIterNext call Exec
   themselves, and the session measures only its own calls, so
   those 360 commands are checked for output agreement alone.
   Metering them needs a delta around the inner call rather than a
   reset around the outer one, which the monotonicity lemmas of
   `MeterSound.lean` would justify.

   The heap meter counts the total bytes of every buffer the call
   allocates, never subtracting: that matches the arena-backed
   targets, where a grown buffer abandons its old block inside the
   arena rather than freeing it. It counts exactly what the
   contract counts, at the same fixed sizes, so it does not model
   the allocator overhead around each block: the runtimes round a
   zero-length request up to one element, and malloc adds its own
   header and alignment. `contract.go` excludes that overhead on
   purpose, because it varies by platform and compiler, and it
   folds a constant factor of slack into its per-record sizes to
   cover it.

   Two corpus patterns cannot be executed under the interpreter
   in any reasonable time, and the theorem leaves their
   executions out: 1056 X commands of the 86691, so it covers
   85635. Both nest a star inside counted repetitions,
   `((a*){250}){250}b` in six blocks and `((a*){4}){4}` in six
   more, and the parse search then explores a very large number of
   ways to split a subject among nullable instances. The cost
   comes from the nesting, not from the subject: measured under
   the interpreter, the first needs about a minute on the empty
   subject and over an hour on a 120 byte one, and the second
   needs minutes on the empty subject. Replaying all twelve blocks
   would take days.

   What stays is chosen so that nothing escapes the check that
   matters. Every compile command of the corpus is kept, all 9779
   of them, so no pattern goes uncompiled and unchecked. The T
   commands of those blocks are kept too, so the contract figures
   of the two extreme patterns are still compared against the Go
   reference; those are the largest figures in the corpus, which
   makes them the ones worth keeping. Only the executions go.
   Dropping them is sound for the session state, because an X
   command allocates its own match buffer and writes no session
   root. `Vego/Corpus.lean` records the measurements, and the
   proposition re-checks its own coverage: it fails if the filter
   ever stops keeping every compile, or matches everything, or
   matches nothing.

   The `vegocheck` executable replays any corpus dump from disk
   through the same code path and enforces the same bounds, which
   is the practical way to check a subset and to see which command
   diverges. `runCorpusFuel` also accepts a fuel bound, but fuel
   caps the recursion depth of each engine call, not a command's
   aggregate work, so it makes a replay deterministic under a
   bound rather than fast.

4. The universal cost lemmas of `Vego/CostLemmas.lean`. These
   quantify over all inputs and are proved by ordinary induction,
   with no native evaluation, so they are not limited to the
   corpus. `growthChain_total` is the geometric series argument on
   Nat: any run of growing appends under the portable rule
   max(2*cap, 8, need) allocates less than twice its final
   capacity across its whole history, and `growCap_le` bounds that
   final capacity by 2n+8 when the needs stay within n. Together
   they justify the doubling constants the contract folds into its
   per-record sizes, against the arena high-water mark, and
   `growthChain_of_steps` connects them to real append histories.
   The layout lemmas prove every element size the meter charges is
   a positive multiple of its alignment, as the shared 64-bit
   layout requires. The exactness lemmas cover every allocation
   form of the language: an in-place append leaves the counter
   unchanged, and a growing append, a make, a slice literal, and
   the two string conversions each raise it by exactly their
   allocation priced at the element size. The `cAdd`/`cMul` lemmas
   pin the saturating contract arithmetic down on its domain: a
   figure below the saturation mark is the exact arithmetic value,
   and no figure ever passes the mark. All of them depend only on
   the standard axioms; none uses native evaluation.

   `Vego/MeterSound.lean` extends this to the whole interpreter,
   by one mutual induction on fuel over every evaluation function:
   on any successful run, no meter counter ever decreases, and the
   call-depth counter returns to its entry value when a call
   completes, so the recorded maximum is the true peak of the run
   (`MOK_evalExpr` through `MOK_runFn`, and `callIdx_meterOK` for
   harness calls). This makes the driver's measurement protocol -
   reset the meter, run one Exec, read the counters - sound by
   proof: the reading can neither under-count an allocation nor
   misattribute the call depth.

   The corpus theorem and these lemmas meet in the middle: the
   lemmas prove the meter and the growth arithmetic right for all
   inputs, and the corpus theorem checks the engine against its
   contract through that meter on every recorded execution. What
   remains corpus-bound is only the engine's own control flow; a
   fully universal contract theorem would need a verified model of
   the matcher, walk and solver loops themselves.

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
Vego/Interp.lean    operational semantics: heap, traps, fuel, meter
Vego/CostLemmas.lean universal theorems about the cost model
Vego/MeterSound.lean meter soundness for the whole interpreter
Vego/Machine.lean   host API: init, call, allocate, read back
Vego/Probe.lean     probe harness (the Lean probe_host)
Vego/Driver.lean    driver protocol session (the Lean driver_host)
Vego/Corpus.lean    the embedded corpus and the replay propositions
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

`lake build` checks every theorem and takes about five minutes.
To find where a change broke things, replay a dump with
`vegocheck` instead: it enforces the same contract bounds and
names the first command that diverges. The toolchain is pinned in
`lean-toolchain`.
