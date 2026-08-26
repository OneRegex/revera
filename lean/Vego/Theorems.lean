/-
The machine-checked statements.

Every theorem here is about the exact JSON artifacts that the Go,
Rust, Zig and C++ printers consume, embedded byte for byte, and
about the formal Vego semantics of Interp.lean. They are proved by
native evaluation (`native_decide`), so the trusted base is the
Lean kernel plus the Lean compiler. The universally quantified
statements about the cost model live in `CostLemmas.lean` and
`MeterSound.lean` instead, and those need no native evaluation.

What the theorems say:

1. `probe_wellformed` and `revera_wellformed`: both programs parse
   from their JSON and elaborate into the typed core. Elaboration
   replays name resolution, operator typing, constant folding and
   width assignment, so success is a well-formedness proof.

2. `probe_agrees`: under the formal semantics, the probe program
   produces, line for line, the reference output of the Go
   original for the whole probe call matrix. The probe matrix
   pins the semantic corners: division overflow, wrapping at all
   widths, evaluation order, spare-capacity zeroing, nil-ness,
   range semantics, struct equality, and view writes.

3. `revera_corpus_agrees_within_contract`: under the formal
   semantics, the revera engine answers the crosscheck corpus
   with exactly the output of the Go reference engine, through
   the same driver protocol as the Zig, C++ and Rust targets:
   compile, execute, replace, iterate, contracts, locale
   selection and case digests. The runs hit no trap: no
   out-of-range index or slice, no division by zero, no
   impossible shift, no ill-typed step.

   Every Exec that an X command performs also stays within the
   resource contract that the engine's own contract code computes
   for its pattern and subject length. The buffer bytes it
   allocates never pass ContractHeapBytes. Its deepest call chain,
   priced at the contract's per-frame estimate, never passes
   ContractStackBytes. Its loop iterations and calls never pass
   ContractSteps. A violation of any bound is a hard session
   fault, so this theorem would be false.

   The R and I commands are outside that claim. ReplaceAll and
   MatchIterNext call Exec themselves, and the session measures
   only the calls it makes directly, so the 360 R and I commands
   of the corpus are checked for output agreement but not against
   a contract. Metering them needs a delta around the inner call
   rather than a reset around the outer one.

   The corpus holds two patterns that cannot be executed under the
   interpreter in any reasonable time, and the theorem leaves
   their 1056 executions out of the 86691 commands. It keeps their
   compiles and their contract queries, so no pattern escapes the
   check and the figures of the extreme cases are still compared
   against the Go reference. `Vego.Corpus` documents the
   measurements behind that choice, and the proposition re-checks
   its own coverage.
-/

import Vego.Probe
import Vego.Corpus

namespace Vego

theorem probe_wellformed : probeChecked.isOk = true := by native_decide

theorem revera_wellformed : reveraChecked.isOk = true := by native_decide

theorem probe_agrees : probeAgrees = true := by native_decide

theorem revera_corpus_agrees_within_contract : corpusAgrees = true := by
  native_decide

end Vego
