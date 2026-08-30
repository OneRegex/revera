/-
The machine-checked statements.

The artifact theorems use the exact JSON that the Go, Rust, Zig and C++ printers consume.
Those artifacts are embedded byte for byte.
The focused harness theorems cover heap generations and strict corpus parsing.
All are proved by native evaluation (`native_decide`), so the trusted base is the Lean kernel plus the Lean compiler.
The universally quantified statements about the cost model live in `CostLemmas.lean` and `MeterSound.lean` instead, and those need no native evaluation.

What the theorems say:

1. `probe_wellformed` and `revera_wellformed`.
   Both programs parse from their JSON and elaborate into the typed core.
   Elaboration replays name resolution, operator typing, constant folding and width assignment, so success is a well-formedness proof.

2. `probe_agrees`.
   Under the formal semantics, the probe program reproduces the reference output of the Go original.
   It matches line for line over the whole probe call matrix.
   The probe matrix pins the semantic corners.
   Those are division overflow, wrapping at all widths, evaluation order, spare-capacity zeroing, nil-ness, range semantics, struct equality, and view writes.

3. `compaction_preserves_stale_references`, `slice_elems_rejects_stale_references`, and the parser theorems.
   Heap compaction does not make an expired pointer or slice readable again.
   Harness slice reads enforce the same generation check as the interpreter.
   A malformed nonempty corpus row fails parsing instead of reducing proof coverage.

4. `revera_corpus_agrees_within_contract`.
   Under the formal semantics, the revera engine answers the crosscheck corpus with exactly the output of the Go reference engine.
   It uses the same driver protocol as the Zig, C++ and Rust targets.
   That protocol covers compile, execute, replace, iterate, contracts, locale selection and case digests.
   The runs hit no trap: no out-of-range index or slice, no division by zero, no impossible shift, no ill-typed step.

   Every Exec that an X command performs also stays within its resource contract.
   The engine's own contract code computes that contract for the pattern and the subject length.
   The buffer bytes it allocates never pass ContractHeapBytes.
   Its deepest call chain, priced at the contract's per-frame estimate, never passes ContractStackBytes.
   Its loop iterations and calls never pass ContractSteps.
   A violation of any bound is a hard session fault, so this theorem would be false.

   The R and I commands are outside that claim.
   ReplaceAll and MatchIterNext call Exec themselves, and the session measures only the calls it makes directly.
   The 360 R and I commands of the corpus are therefore checked for output agreement, but not against a contract.
   Metering them needs a delta around the inner call rather than a reset around the outer one.

   The corpus holds two patterns that cannot run under the interpreter in any reasonable time.
   The theorem leaves their 1056 executions out of the 86704 commands.
   It keeps their compiles and their contract queries, so no pattern escapes the check.
   The figures of the extreme cases still compare against the Go reference.
   `Vego.Corpus` records the measurements behind that choice, and the proposition re-checks its own coverage.
-/

import Vego.Probe
import Vego.Corpus

namespace Vego

theorem probe_wellformed : probeChecked.isOk = true := by native_decide

theorem revera_wellformed : reveraChecked.isOk = true := by native_decide

theorem probe_agrees : probeAgrees = true := by native_decide

private def regressionProgram : TProgram := {
  structNames := #[]
  structFields := #[]
  structs := #[]
  globalInits := #[]
  funcs := #[]
}

private def regressionMachine : Machine := {
  prog := regressionProgram
  ctx := { structs := #[], funcs := #[], globals := #[] }
  heap := { Heap.empty with cells := #[(0, .b false), (7, .arr #[.i 42])] }
}

private def migratedLocationIsStale (root : Val) : Bool :=
  let old : Array Cell := #[(0, .b false), (7, .i 42)]
  let memo := Array.replicate old.size none
  let fresh : Array Cell := #[(0, .b false)]
  let (root, _, cells) := migrateVal old memo fresh root
  let stale (obj gen : Nat) (path : Path) :=
    match M.readLoc obj gen path { Heap.empty with cells } with
    | .trap .stale => true
    | _ => false
  match root with
  | .ptr obj gen path =>
    stale obj gen path
  | .slice (some (obj, gen, path)) _ _ _ =>
    stale obj gen path
  | _ => false

private def migrationPreservesStale : Bool :=
  migratedLocationIsStale (.ptr 1 0 []) &&
  migratedLocationIsStale (.slice (some (1, 0, [])) 0 1 1)

theorem compaction_preserves_stale_references : migrationPreservesStale = true := by
  native_decide

theorem slice_elems_rejects_stale_references :
    regressionMachine.sliceElems (.slice (some (1, 0, [])) 0 1 1) = none := by
  native_decide

theorem corpus_parser_rejects_malformed_rows :
    (match parseCorpus "P\tP 1\nmalformed" with
     | .error msg => msg == "malformed corpus row 2"
     | .ok _ => false) = true := by
  native_decide

theorem corpus_parser_accepts_valid_rows :
    (match parseCorpus "P\tP 1\n\nQ\tQ 2\n" with
     | .ok rows => rows == [("P", "P 1"), ("Q", "Q 2")]
     | .error _ => false) = true := by
  native_decide

theorem revera_corpus_agrees_within_contract : corpusAgrees = true := by
  native_decide

end Vego
