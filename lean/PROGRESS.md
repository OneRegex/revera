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
- [x] `lake build` with all four theorems checked: well-formedness
      of both artifacts, the 29-line probe agreement, and the
      86691-command corpus agreement, all by native_decide, each
      depending only on propext, Classical.choice, Quot.sound and
      its native_decide axiom
- [x] Unbounded full-corpus replay of vegocheck (confirmation run)
