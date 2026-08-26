# LEAN4 model progress

## Findings and decisions

- The JSON grammar of VEGO-SPECIFICATION.md section 8 maps onto a
  small mutual AST (`Ast.lean`); both shipped artifacts decode with
  a total, fuel-bounded decoder. All string literals in both
  programs are ASCII, so JSON string values equal their byte
  content.
- Rather than interpret the raw tree with runtime type guessing,
  the model elaborates it into a typed core first (`Elab.lean`):
  names resolve to indices, every arithmetic node carries its
  wrapping width, untyped constants fold exactly (Go's rules:
  unbounded integers, truncated division), composite literals gain
  their zero fills, and `return f()` forwarding is recognized.
  Elaboration success doubles as the well-formedness theorem.
- The semantics (`Interp.lean`) uses a heap of cells so slices and
  borrows alias exactly as in Go: a slice header is
  (cell, path, off, len, cap); views of local arrays point into the
  variable's cell. The interpreter is total via a fuel argument;
  Lean's termination checker accepts the whole mutual block
  structurally once no recursion hides inside `forIn` closures.
- Buffer growth follows the portable runtime contract that the
  Zig, C++ and Rust runtimes implement (`vg.zig`: newcap =
  max(2*cap, 8, need), zeroed spare), not Go's internal growth
  curve. `cap` after a growing append therefore differs between
  the Go original and every other target; the swival review made
  this concrete (`append(nil, 1)` on `[]int64`: Go 4, others 8).
  The specification now states the rule: such capacities are
  target defined and must not reach observable output. The revera
  engine's one post-growth `cap` read (`capture.go` kidAlloc) only
  picks between two equivalent paths, and the cross-target
  corpus agreement checks exactly that.
- The Go original's own probe host, driver host, and crosscheck
  corpus provide the reference outputs. The Lean harnesses
  (`Probe.lean`, `Driver.lean`) mirror probe_host.go and
  driver_host.go call for call, including the FNV case digest and
  the session state machine.
- Two real bugs found during bring-up, both in the Lean model, not
  in the vego artifacts: the elaborator dropped the slot counter of
  nested blocks (frames too small), and a first `findCase` draft
  recursed through a placeholder. The probe matrix caught both at
  first run. After the frame fix, all 29 probe lines agreed on the
  first complete run, and the first 66000+ corpus commands agreed
  on the first run.
- The first pathological slowdown was not inherent: `sample`
  showed `lean_copy_expand_array` on top, and the cause was Lean's
  borrow inference passing `Val.store`'s argument borrowed, so
  every buffer-element write copied the containing array. Inlining
  the one- and two-step write paths into `M.writeLoc` (with the
  touched slot detached first) took the worst measured command
  from 104 s to 0.4 s and the heavy fixed-pattern block from 412 s
  to 1.65 s. The remaining slow commands are real work: the
  `((a*){250}){250}b` executions walk a 62500-node automaton and
  cost minutes at the interpreter's ~200x factor over Go.
- Fuel in the interpreter bounds the recursion depth, not the step
  count (siblings each receive fuel - 1), so a fuel budget cannot
  cap wall time. The corpus theorem therefore replays the whole
  corpus unbounded instead of skipping by budget.
- The resource contracts needed a unit for "abstract steps" that
  the interpreter can observe. Counting every executed statement
  is too fine: a one-byte subject pays Exec's fixed setup and
  passes the contract figure (worst 1.2x on the corpus). Counting
  one unit per loop iteration and per call matches the granularity
  contract.go describes, and the straight-line code between two
  such units is bounded by the artifact's text; on the corpus the
  worst case stays well under the bound. The heap meter counts
  total bytes ever allocated, never subtracting, which matches the
  arena-backed targets, since their grow() abandons the old block
  inside the arena. It counts what the contract counts and no
  more, so it models no allocator overhead: the runtimes round a
  zero-length request up to one element and malloc adds a header.
  contract.go leaves that out on purpose, as platform dependent.
- The stack contract has no headroom on one corpus case, and the
  measurement found it. Over 69248 measured Exec calls the worst
  heap margin is 60 percent of the bound and the worst loop margin
  27 percent, but the worst stack margin is exactly 100 percent:
  pattern `[[=ch=]]` on subject "HhhxhH" reaches 18 interpreted
  call frames against a bound of 18. The figures agree by
  construction, since `matcherContract` prices phase A as
  matcherStackBytes (2048, eight frames) plus equivFrames
  (maxElemAhead + 2 = 10) frames, and the multi-character
  equivalence test really does recurse once per element character.
  The bound holds, because the check rejects only a run that
  passes it. It leaves nothing spare though, so any extra frame on
  the phase A path breaks the contract, and the equivFrames slack
  should grow before that happens.
- The contract check covers the X commands only. ReplaceAll and
  MatchIterNext call Exec inside the engine, so the 360 R and I
  commands of the corpus run Exec calls the session never meters;
  they are checked for output agreement alone. Covering them needs
  a delta around the inner call, snapshot before and subtract
  after, rather than the reset the X handler uses. The
  monotonicity lemmas of MeterSound.lean already justify that
  form. The theorem and the READMEs now say X rather than "every
  Exec".
- The contract figures of the two intractable patterns are the
  largest the corpus produces, but they do not saturate:
  `((a*){4}){4}` reports 6.56e12 steps at maxinput 4096 against a
  cap of 4.6e18. An earlier note claiming they reach the cap was
  wrong, and no threshold on ContractSteps separates the
  intractable blocks from tractable ones, which is why the
  exclusion list names patterns instead.
- The corpus is not uniformly cheap to replay, and the old
  promise of "tens of minutes" for the corpus theorem was wrong by
  orders of magnitude. Twelve compile blocks, six of
  `((a*){250}){250}b` and six of `((a*){4}){4}`, would take days
  between them, while the other 85635 commands replay in about
  five minutes. The cost comes from the nesting, not the subject:
  each block holds four 120 byte subjects, one of which was
  measured at 107 minutes, but `((a*){4}){4}` needs minutes even
  on the empty subject. The theorem now drops those 1056
  executions and keeps everything else, compiles and contract
  queries included.

## Status

- [x] AST + total JSON decoder; both artifacts decode
- [x] Typed core + elaborator; both artifacts elaborate
- [x] Operational semantics (total, fuel-bounded)
- [x] Machine/harness API
- [x] Probe harness: 29/29 lines agree with the Go original
- [x] Driver protocol session + heap compaction
- [x] crosscheck -dumpexpected; corpus with reference outputs
- [x] Fuel-budgeted corpus runner with deterministic skips
- [x] Write-path profile fix (in-place buffer writes)
- [x] Generation-tagged frame recycling (bounded memory, dangling
      views trap as `stale` instead of misreading)
- [x] All four theorems checked by native_decide, each depending
      only on propext, Classical.choice, Quot.sound and its
      native_decide axiom
- [x] vegocheck replays of the corpus with contract enforcement:
      85599 commands in one run, plus the first 14 commands of an
      `((a*){250}){250}b` block, all agreeing
- [x] Resource meter in the interpreter (buffer bytes at the
      shared 64-bit layout, call depth, loop and call steps) and
      the per-Exec contract check in the driver session; the
      corpus theorem now also states that no Exec in the corpus
      exceeds the contract of the engine's own ContractFor
- [x] vegocheck --contracts calibration mode: replays a dump with
      enforcement off and reports the tightest margins per meter
- [x] Universal cost lemmas (Vego/CostLemmas.lean), proved by
      induction with no native evaluation: the geometric bound of
      the append growth rule and its connection to real append
      histories, the layout well-formedness, the exactness of the
      allocation meter on every allocation form (append, make,
      slice literal, both string conversions), and the saturation
      algebra of the contract arithmetic
- [x] `Vego/Corpus.lean` derives the replay set from the embedded
      corpus: everything except the 1056 executions of the two
      intractable patterns, so all 9779 compiles and every
      contract query stay in. The proposition re-checks its own
      coverage
- [x] `lake build` finishes in about five minutes and checks all
      four theorems, the corpus contract theorem among them
- [x] Meter soundness for the whole interpreter
      (Vego/MeterSound.lean): one mutual induction on fuel proves
      that no counter ever decreases and that the call depth
      balances across every successful call, up to the harness
      corollary callIdx_meterOK; the driver's reset-run-read
      measurement is thereby sound by proof
