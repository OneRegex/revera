# The Lean 4 model of Vego and the ERE specification

This directory holds the Lean 4 formalization that the Vego specification promises, and a formal model of the ERE specification the engine implements.

It gives the subset a formal semantics.
It applies that semantics to the exact JSON artifacts that the Go, Rust, Zig, C++, and C11 printers consume.
It states the POSIX ERE contract of `docs/POSIX-1-2024-ERE-SPECIFICATION.md` as a Lean definition, and checks the interpreted engine against that definition.

The theorems in `Vego/Theorems.lean` are machine checked.

## What is proved

The theorems of `Vego/Theorems.lean` are about the embedded copies of `vego/probe/probe.vego.json` and of `revera.vego.json` at the repository root, byte for byte.
Native evaluation checks them.
The Lake configuration tracks every embedded JSON, corpus, locale, and probe input, so changing one rebuilds the modules that contain it.

By contrast, the theorems of `Vego/CostLemmas.lean`, `Vego/MeterSound.lean`, `Vego/PhaseAProofs.lean` and `Vego/PhaseARun.lean` are universal rather than corpus enumerations.
They are proved by induction, with no native evaluation.
Their hypotheses are stated in sections 5 through 7.

### 1. The artifacts are well formed

`probe_wellformed` and `revera_wellformed`.

Both programs decode from JSON and elaborate into a fully typed core.
Elaboration replays what the Go compiler and `vegoc check` guarantee.

Every name resolves, and every operator gets one width.
Untyped constants fold exactly and fit their contexts, composite literals are complete, and control flow is well shaped.
Consequently, success is a well-formedness proof for the shipped artifacts.

### 2. The probe agrees with the Go original

`probe_agrees`.

Under the formal semantics, the probe program reproduces the 29 report lines of the Go original.
Those lines live in `lean/data/probe.expected`, from `dev/internal/conformance/proberef`.

The probe matrix pins the semantic corners of the language.
Those corners are division overflow, wrapping at every width, evaluation order, spare-capacity zeroing, nil-ness, range copies, struct equality with string arrays, and writes through subslice views.

### 3. The engine agrees, and stays inside its contract

`revera_corpus_agrees_within_contract`.

Under the formal semantics, the Revera engine answers the retained commands of the embedded crosscheck corpus exactly like the Go reference.
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

That covers the 76,112 retained X commands.
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
Instead, it folds a constant factor of slack into its per-record sizes to cover it.

Two corpus patterns cannot run under the interpreter in any reasonable time, and the theorem leaves their executions out.
That is 1,056 X commands of the 86,704, so the theorem covers 85,648 commands.

Both nest a star inside counted repetitions, `((a*){250}){250}b` in six blocks and `((a*){4}){4}` in six more.
The parse search then explores a very large number of ways to split a subject among nullable instances.
The cost comes from the nesting, not from the subject.

Under the interpreter, the first needs about a minute on the empty subject and over an hour on a 120 byte one.
The second needs minutes on the empty subject.
Therefore, replaying all twelve blocks would take days.

What stays is chosen so that nothing escapes the check that matters.
Every compile command of the corpus stays, all 9,780 of them, so no pattern goes uncompiled and unchecked.
The T commands of those blocks stay too, so the contract figures of the two extreme patterns still compare against the Go reference.
Those are the largest figures in the corpus, which makes them the ones worth keeping.

Only the executions go, and dropping them is sound for the session state.
An X command allocates its own match buffer and writes no session root.

`Vego/Corpus.lean` records the measurements, and the proposition re-checks its own coverage.
It fails if the filter ever stops keeping every compile, or matches everything, or matches nothing.

The `vegocheck` executable replays any corpus dump from disk through the same code path and enforces the same bounds.
That is the practical way to check a subset and see which command diverges.

`runCorpusFuel` also accepts a fuel bound.
Fuel caps the recursion depth of each engine call, not the aggregate work of a command.
It therefore makes a replay deterministic under a bound, rather than fast.

### 4. The engine meets the ERE specification

`spec_agrees_with_corpus`, `engine_meets_spec_on_corpus` and `engine_meets_spec_exhaustively`.

The `Ere/` library is a model of the specification, written from its text and not from any engine.
`Ere/Syntax.lean` is the grammar of sections 5 to 7.
It classifies every pattern as defined, invalid, or free, where free covers the undefined spellings of section 14.2, the unspecified choices of section 14.3, and anything outside the interface domain.

`Ere/Semantics.lean` is the match relation.
It lists every derivation of a pattern over a span of the subject, orders them by the rules of section 4.3, and reads the captures off the chosen one by the rules of section 12.7.
The empty-occurrence rule of section 8.5, the anchors and newline mode of sections 9.2 and 12.3, and the case-insensitive closure of section 10.2 are each one definition with the section named.

`Ere/Locale.lean` is the POSIX locale of sections 7.6, 7.7 and 10.2, behind the locale adapter of section 15.1.
`Ere/Examples.lean` checks every example of section 16 against the model.
`Ere/Lemmas.lean` proves that the computable closure over case preimages is the closure rule of section 10.2 in the POSIX locale, for every acceptance predicate.

`spec_agrees_with_corpus` walks the corpus commands in order, as the driver session does, and states what the specification requires of each output.
It reads only the commands, never the recorded outputs.
Every constrained command's recorded output meets its verdict.

The walk leaves a command unconstrained in exactly the counted cases: the locale is not POSIX, the pattern is free, the subject holds a NUL or is not valid UTF-8, or the enumeration ran out of budget.
`expectedCoverage` pins the figures, so any drift in coverage fails the theorem.

On the current corpus, all 9,653 defined patterns compile with the specification's subexpression count.
In addition, all 66 free spellings in the corpus are the ones the engine rejects.
Finally, 66,576 executions produce the specified line.
The 1,512 executions left out are the ones on subjects with NUL bytes or invalid UTF-8, which are not strings of the interface.

`engine_meets_spec_on_corpus` composes that theorem with the corpus theorem.
For every corpus command the specification constrains, the interpreted engine prints a line that meets the specification's verdict.
The recorded Go outputs are the bridge between the two native evaluations.
Because both sides are checked against those outputs, the Go engine is not in the trust base of the composed statement.

`engine_meets_spec_exhaustively` compares the two directly, on inputs no engine has seen.
`Vego/Exhaustive.lean` runs two sweeps over a token language of fourteen tokens: the atoms `a`, `b`, `.`, `[ab]` and `[^a]`, parentheses, `|`, the duplications `*`, `+`, `?` and `{0,1}`, and both anchors.

The structure sweep takes every string of one through four tokens, which is 41,370 patterns, and every subject over `{a, b}` of up to three characters.
It tests each pair with and without `REG_MINIMAL` and with and without the two execution flags.
The flag sweep takes every string of one through three tokens under all sixteen compile flag combinations and all four execution flag combinations, on subjects that add newlines and uppercase letters.

Each defined pattern is compiled under the formal Vego semantics and executed on each subject, and the line must be the one the specification requires.
That is 42,128 compiles and 1,576,896 executions, and `coverageStructure` and `coverageFlags` pin those figures.

What these theorems do not cover is stated as plainly.
Non-POSIX locales are outside the model, because sections 7.7 and 14.3 leave ranges and multi-character lists to the implementation there.
The `R`, `I` and `T` commands are engine interfaces without a POSIX counterpart.
Finally, all three statements are finite: they quantify over the corpus and the enumerated domain, not over every pattern and subject.

### 5. The phase A heap and step bounds are universal

`PhaseA.run_steps_le`, `PhaseA.run_heap_le` and `phaseA_link_agrees`.

`Vego/PhaseA.lean` models the phase A matcher of `engine.go` function for function.
It covers the slot tables and their generation stamps, the relaxation queue and its compaction, the merge order of payloads, the consuming transitions with their counter increments, the scan filter and the early stop.
It is parametric in the compiled program, in the atom tests, and in the subject, and it meters its own events.
`stepFigure` prices those events one unit per loop iteration and per call, the way the interpreter's loop meter prices the Vego code.

`Vego/PhaseAProofs.lean` proves two bounds for every well-formed program, every atom test and every subject, by induction and with no evaluation.
The step figure of a run stays under `stepsFigure`, and the bytes it allocates stay under `heapFigure`.

The closure is the hard part.
Its cost is paid by a potential: the queue length plus, for every instruction of the slot, the rank of its stored payload among the payloads known at the boundary.
A successful merge stores a strictly better payload and lowers a rank, every pop shortens the queue, and a compaction is paid by the entries it drops.

The buffers are bounded through the growth rule.
The active list of a slot holds distinct instructions, while the queue never passes twice the program length plus one.
The geometric argument of `CostLemmas.lean` then turns those needs into capacities.

`phaseA_link_agrees` ties the model to the engine.
The link covers every corpus execution that runs phase A alone, which means every `NoSub` pattern and every pattern without a group.
On those executions, the model runs on the program read from the interpreted `Regexp` and reproduces the engine's match result.
It also allocates exactly the bytes that the interpreter counts, while its step figure dominates the interpreter's loop meter.

On every `T` command of such a pattern, the heap and step figures the engine reports equal the proven figures.
`linkCoverage` pins the counts: 47,922 executions, 46 contract queries, and 6,893 programs that pass `Prog.wfCheck`.

On the linked corpus executions, `contract.go` computes exactly `heapFigure` and `stepsFigure` for phase A.
Those are the bounds proved universally for the model under the well-formedness hypotheses above.

Two things led to this.
First, an adversarial probe found that the previous phase A heap figure was unsound.
Under the portable growth rule, `(a{0,8}){0,8}b` on `aaaaaaaa` allocated 7,546 bytes against a figure of 7,290 because the doubling of the active lists and queue was folded in too tightly.
Second, the proof showed what the constants must be, and the per-boundary step budget grew with them.

The non-POSIX locales and the multi-character probes are inside the universal theorems but outside the corpus link, which covers the POSIX locale.
Phase B, the capture solver, keeps its corpus-bound contract check.

### 6. The phase A result is correct under stated hypotheses

`phaseA_run_correct`, from `PhaseA.run_correct` of `Vego/PhaseARun.lean`.

The contract theorem of section 5 says what a run costs.
This one says what it answers.
`Vego/PhaseACorrect.lean` states the reference the model is judged against, and that reference knows nothing of slots, generations or queues.

A thread is a boundary index of the subject, an instruction, the start it carries and its counters.
It can step along an epsilon edge of the program, where `^` and `$` read the anchors from the subject.
Alternatively, it consumes one character when the single test accepts it, or a collating element of several characters when a bracket probe does.
Consuming steps bump the counters the way the engine does.
A thread is reached when a spawn at some boundary of the subject steps to it.

A candidate is a reached thread at the accept instruction, taken as its start, its counters and its end position.
The selection order is the engine's: earliest start, then smallest counters, then longest end.
When the model reports a match, `run_correct` says that its start, end and retained counters form a candidate that the order puts at or before every other candidate.
When the model reports no match, the theorem says that no candidate exists.

The proof is an invariant over the boundary loop.
Every fresh entry of a slot is a thread the reference reached, so the model never invents a match.
Every productive thread, one that can still reach accept, is covered: the slot of its boundary holds a payload at or below it in the merge order, or the best match already known dominates it.

The first half is the merge argument.
A store only replaces a payload with a better one, and a bump preserves the order.
The second half is why pruning is safe.
A dominated payload cannot produce a better candidate, because bumps only raise counters and the best only improves.

The closure at a boundary is proved by the same drain induction as the cost bound, with a queued-or-done invariant per fresh instruction.
The consuming phase then delivers each arrival to the slot of its target boundary.
The early stop is justified when no future slot remains live, because no remaining path can beat the best match.

The scan filter's jump is proved under `ScanSound`, an assumption stated against the reference.
When the filter is enabled, the ring has two slots, and a jump lands on a subject boundary after skipping only boundaries whose spawns cannot accept.
That assumption is about the compiler's choice of stop bytes, which the model does not contain, so it stays a hypothesis.

The other hypotheses cover program well-formedness and the fit of multi-character probes in the ring.
`Prog.wfCheck` decides the well-formedness condition, which `phaseA_link_agrees` now checks on every corpus program.
The theorem depends on `propext`, `Classical.choice` and `Quot.sound` only.

### 7. The cost model holds for every input

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

`Vego/MeterSound.lean` extends this result to the whole interpreter.
It uses one mutual induction on fuel over every evaluation function.
On any successful run, no meter counter ever decreases.
The call-depth counter returns to its entry value when a call completes, so the recorded maximum is the true peak of the run.
The lemmas are `MOK_evalExpr` through `MOK_runFn`, and `callIdx_meterOK` for harness calls.

This makes the measurement protocol of the driver sound by proof.
The driver resets the meter, runs one Exec, and reads the counters.
That reading can neither under-count an allocation nor misattribute the call depth.

The corpus theorem and these lemmas meet in the middle.
The lemmas prove the meter and the growth arithmetic right for all inputs.
The corpus theorem checks the engine against its contract through that meter on every retained Exec command.

The phase A cost bounds are universal under program and atom-test well-formedness.
Its result theorem additionally assumes that multi-character probes fit the ring and that the scan filter satisfies `ScanSound`.
What remains corpus-bound is the control flow of the walk and of the capture solver.

## What this means for the pipeline

The generated Rust, Zig, C++, and C11 engines come from the same JSON that the theorems cover.
`dev/internal/conformance/crosscheck` verifies all four against the Go engine on the same corpus.

On the covered corpus commands, the Lean semantics therefore anchors the whole chain.
One side runs JSON through the formal semantics to the recorded outputs.
The other side runs the same JSON through a printer into a target engine and reaches the same outputs through the conformance tests.

## Structure

```
Ere/Locale.lean     the locale adapter of section 15.1 and the POSIX locale
Ere/Syntax.lean     the ERE grammar of sections 5 to 7, as a classifying parser
Ere/Semantics.lean  derivations, the selection order of section 4.3, captures of section 12.7
Ere/Lemmas.lean     the closure rule of section 10.2, proved for every predicate
Ere/Examples.lean   the examples of section 16 as theorems
Vego/Ast.lean       raw syntax tree, the image of the JSON grammar
Vego/Decode.lean    total JSON decoder
Vego/Data.lean      the two embedded .vego.json artifacts
Vego/Core.lean      typed core: resolved names, widths, zero fills
Vego/Elab.lean      elaborator (checker): raw tree to typed core
Vego/Interp.lean    operational semantics: heap, traps, fuel, meter
Vego/PhaseA.lean    the phase A matcher, modeled with its meter
Vego/PhaseAProofs.lean universal phase A heap and step bounds for well-formed programs and atom tests
Vego/PhaseACorrect.lean the reference semantics of a program, and the closure of one boundary proved against it
Vego/PhaseARun.lean the phase A result under well-formedness, ring-fit and ScanSound hypotheses
Vego/PhaseALink.lean the model against the interpreted engine on the corpus
Vego/CostLemmas.lean universal theorems about the cost model
Vego/MeterSound.lean meter soundness for the whole interpreter
Vego/Machine.lean   host API: init, call, allocate, read back
Vego/Probe.lean     probe harness (the Lean probe_host)
Vego/Driver.lean    driver protocol session (the Lean driver host)
Vego/CorpusData.lean the embedded corpus and the replay set
Vego/Corpus.lean    the replay proposition of the corpus theorem
Vego/SpecCheck.lean the specification's verdict on every corpus command
Vego/Exhaustive.lean the interpreted engine against the specification on a small domain
Vego/Theorems.lean  the machine-checked statements
Main.lean           vegocheck: replay any corpus dump from disk
SpecMain.lean       speccheck: the specification's verdicts on a corpus dump, with mismatches
ExhMain.lean        exhaustcheck: the exhaustive sweep, naming the first disagreement
LinkMain.lean       phasealink: the phase A model against the engine on a corpus dump
data/               probe.expected, corpus.tsv, localedata.hex
```

## The semantics in brief

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

The artifact, corpus and exhaustive theorems use `native_decide`, so they rest on the Lean kernel plus the Lean compiler, through `Lean.ofReduceBool`.
The Go engine produces the reference outputs of the corpus theorem.

The specification theorems replace that reference with the `Ere` model for every command they cover.
On those commands, the Go engine is therefore outside the trust base, while the specification model is inside it.
The model is meant to be read against the specification, section by section, and `Ere/Examples.lean` checks it against the examples of section 16.

The specification theorems are finite: they cover the corpus and the enumerated domain, not every pattern.

The elaborator does not re-check the buffer-ownership clauses that `vegoc check` leaves to review, which are moves and rule 9.
The semantics does not need them, because it models aliasing directly.

## Regenerating the data

```sh
cd dev
go run ./internal/conformance/proberef > ../lean/data/probe.expected
go run ./internal/conformance/crosscheck -dumpexpected ../lean/data/corpus.tsv
xxd -p ../go/data.bin | tr -d '\n' > ../lean/data/localedata.hex
cd ../lean && lake build && .lake/build/bin/vegocheck data/corpus.tsv
```

`lake build` checks every theorem.
A warm rebuild is fast, while a change to an embedded input reruns the native evaluations.

To find where a change broke things, replay a dump with `vegocheck` instead.
It enforces the same contract bounds and names the first command that diverges.

`speccheck data/corpus.tsv` walks a dump under the specification and prints the coverage and every mismatch.
`exhaustcheck` runs the two exhaustive sweeps and names the first disagreement, and `exhaustcheck 500` limits each sweep to its first 500 patterns.
`phasealink data/corpus.tsv` runs the phase A model against the engine on a dump and reports the worst ratios of the model's figures to the contract.

`lean-toolchain` pins the toolchain.
