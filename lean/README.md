# The LEAN4 model of Vego

This directory holds the Lean 4 formalization that the Vego specification promises.

It gives the subset a formal semantics.
It applies that semantics to the exact JSON artifacts that the Go, Rust, Zig and C++ printers consume.
The theorems in `Vego/Theorems.lean` are machine checked.

## What is proved

The theorems of `Vego/Theorems.lean` are about the embedded copies of `go1/probe.vego.json` and `go1/revera.vego.json`, byte for byte.
Native evaluation checks them.
The Lake configuration tracks every embedded JSON, corpus, locale, and probe input, so changing one rebuilds the modules that contain it.
The theorems of `Vego/CostLemmas.lean` and `Vego/MeterSound.lean` quantify over all inputs and are proved by induction, with no native evaluation.

### 1. The artifacts are well formed

`probe_wellformed` and `revera_wellformed`.

Both programs decode from JSON and elaborate into a fully typed core.
Elaboration replays what the Go compiler and vego2json guarantee.
Every name resolves, and every operator gets one width.
Untyped constants fold exactly and fit their contexts, composite literals are complete, and control flow is well shaped.
Success is therefore a well-formedness proof for the shipped artifacts.

### 2. The probe agrees with the Go original

`probe_agrees`.

Under the formal semantics, the probe program reproduces the 29 report lines of the Go original.
Those lines live in `lean/data/probe.expected`, from `cmd/proberef`.
The probe matrix pins the semantic corners of the language.
Those corners are division overflow, wrapping at every width, evaluation order, spare-capacity zeroing, nil-ness, range copies, struct equality with string arrays, and writes through subslice views.

### 3. The engine agrees, and stays inside its contract

`revera_corpus_agrees_within_contract`.

Under the formal semantics, the revera engine answers the embedded crosscheck corpus exactly like the Go reference.
That corpus is `lean/data/corpus.tsv`, which pairs each command with the output of the Go engine.
The replay uses the same driver protocol as the Zig, C++ and Rust targets: compile, execute, replace, iterate, contracts, locale selection and case digests.
The runs hit no trap anywhere in the millions of interpreted operations these commands execute.
There is no out-of-range index or slice, no division by zero, no impossible shift, and no ill-typed step.

The same theorem proves the resource contracts sound over the executions it measures.
The interpreter meters each Exec the session calls.
It counts the bytes of every buffer allocation, the deepest call chain, and one abstract step per loop iteration and per call.
The allocation count covers make, append growth, slice literals and the string conversions, at the fixed 64-bit layout the targets share.
Before each call, the session computes the contract of the pattern for the subject length, with the engine's own interpreted `ContractFor`.
A measurement above `ContractHeapBytes`, `ContractStackBytes` or `ContractSteps` is a hard fault.
The stack comparison uses the 256-byte frame estimate of the contract.

That covers the X commands, which are 76112 of the corpus.
It does not cover R and I.
ReplaceAll and MatchIterNext call Exec themselves, and the session measures only its own calls.
Those 360 commands are therefore checked for output agreement alone.
Metering them needs a delta around the inner call, rather than a reset around the outer one.
The monotonicity lemmas of `MeterSound.lean` would justify that delta.

The heap meter counts the total bytes of every buffer the call allocates, and never subtracts.
That matches the arena-backed targets, where a grown buffer abandons its old block inside the arena instead of freeing it.
It counts exactly what the contract counts, at the same fixed sizes, so it does not model the allocator overhead around each block.
The runtimes round a zero-length request up to one element, and malloc adds its own header and alignment.
`contract.go` leaves that overhead out on purpose, because it varies by platform and compiler.
It folds a constant factor of slack into its per-record sizes to cover it.

Two corpus patterns cannot run under the interpreter in any reasonable time, and the theorem leaves their executions out.
That is 1056 X commands of the 86704, so the theorem covers 85648.
Both nest a star inside counted repetitions, `((a*){250}){250}b` in six blocks and `((a*){4}){4}` in six more.
The parse search then explores a very large number of ways to split a subject among nullable instances.
The cost comes from the nesting, not from the subject.
Under the interpreter, the first needs about a minute on the empty subject and over an hour on a 120 byte one.
The second needs minutes on the empty subject.
Replaying all twelve blocks would take days.

What stays is chosen so that nothing escapes the check that matters.
Every compile command of the corpus stays, all 9780 of them, so no pattern goes uncompiled and unchecked.
The T commands of those blocks stay too, so the contract figures of the two extreme patterns still compare against the Go reference.
Those are the largest figures in the corpus, which makes them the ones worth keeping.
Only the executions go, and dropping them is sound for the session state.
An X command allocates its own match buffer and writes no session root.
`Vego/Corpus.lean` records the measurements, and the proposition re-checks its own coverage.
It fails if the filter ever stops keeping every compile, or matches everything, or matches nothing.

The `vegocheck` executable replays any corpus dump from disk through the same code path and enforces the same bounds.
That is the practical way to check a subset, and to see which command diverges.
`runCorpusFuel` also accepts a fuel bound.
Fuel caps the recursion depth of each engine call, not the aggregate work of a command.
It therefore makes a replay deterministic under a bound, rather than fast.

### 4. The cost model holds for every input

The lemmas of `Vego/CostLemmas.lean` quantify over all inputs, and ordinary induction proves them.
They use no native evaluation, so the corpus does not limit them.

`growthChain_total` is the geometric series argument on Nat.
Any run of growing appends under the portable rule `max(2*cap, 8, need)` allocates less than twice its final capacity, across its whole history.
`growCap_le` bounds that final capacity by `2n+8` when the needs stay within n.
Together they justify the doubling constants that the contract folds into its per-record sizes, against the arena high-water mark.
`growthChain_of_steps` connects them to real append histories.

The layout lemmas prove that every element size the meter charges is a positive multiple of its alignment, as the shared 64-bit layout requires.
The exactness lemmas cover every allocation form of the language.
An in-place append leaves the counter unchanged.
A growing append, a make, a slice literal, and the two string conversions each raise it by exactly their allocation, priced at the element size.
The `cAdd` and `cMul` lemmas pin the saturating contract arithmetic down on its domain.
A figure below the saturation mark is the exact arithmetic value, and no figure ever passes the mark.

`Vego/MeterSound.lean` extends this to the whole interpreter, by one mutual induction on fuel over every evaluation function.
On any successful run, no meter counter ever decreases.
The call-depth counter returns to its entry value when a call completes, so the recorded maximum is the true peak of the run.
The lemmas are `MOK_evalExpr` through `MOK_runFn`, and `callIdx_meterOK` for harness calls.
This makes the measurement protocol of the driver sound by proof.
The driver resets the meter, runs one Exec, and reads the counters.
That reading can neither under-count an allocation nor misattribute the call depth.

The corpus theorem and these lemmas meet in the middle.
The lemmas prove the meter and the growth arithmetic right for all inputs.
The corpus theorem checks the engine against its contract through that meter, on every recorded execution.
What remains corpus-bound is only the control flow of the engine itself.
A fully universal contract theorem would need a verified model of the matcher, walk and solver loops.

## What this means for the pipeline

The generated Rust, Zig and C++ engines come from the same JSON the theorems talk about.
`cmd/crosscheck` verifies all three against the Go engine on the same corpus.
The Lean semantics therefore anchors the whole chain.
One side runs JSON through the formal semantics to the reference outputs.
The other side runs the same JSON through a printer into a target engine, and reaches the same outputs.

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

Memory is a heap of cells.
Each local variable lives in a cell.
Each buffer from make, append, a slice literal or a byte conversion lives in its own cell.
A slice value is a header of cell, path, offset, length and capacity.
Views therefore alias exactly as in Go, views of local arrays included.
Values carry canonical integers, and every operation wraps at the width the elaborator assigned.
Abnormal terminations are traps, and the interpreter is total: it recurses on fuel and reports exhaustion as a trap.
Fuel bounds the depth of one engine call, not its aggregate work.
Growth follows the portable runtime contract of the targets, `max(2*cap, 8, need)` with a zeroed spare region.
The Go original grows with the append of Go, so capacities after growth differ.
The specification declares them target defined and keeps them out of observable output.

## Trust base

The proofs use `native_decide`, so they rest on the Lean kernel plus the Lean compiler, through `Lean.ofReduceBool`.
The Go engine produces the reference outputs.
The theorems state agreement with those recorded outputs, not POSIX correctness in the abstract.
The elaborator does not re-check the buffer-ownership clauses that vego2json leaves to review, which are moves and rule 9.
The semantics does not need them, because it models aliasing directly.

## Regenerating the data

```
cd go1
go run ./cmd/proberef > ../lean/data/probe.expected
go run ./cmd/crosscheck -dumpexpected ../lean/data/corpus.tsv
xxd -p revera/data.bin | tr -d '\n' > ../lean/data/localedata.hex
cd ../lean && lake build && .lake/build/bin/vegocheck data/corpus.tsv
```

`lake build` checks every theorem and takes about five minutes.
To find where a change broke things, replay a dump with `vegocheck` instead.
It enforces the same contract bounds and names the first command that diverges.
`lean-toolchain` pins the toolchain.
