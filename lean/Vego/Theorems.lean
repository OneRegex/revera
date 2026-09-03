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
   The theorem leaves their 1056 executions out of the 90145 commands.
   It keeps their compiles and their contract queries, so no pattern escapes the check.
   The figures of the extreme cases still compare against the Go reference.
   `Vego.Corpus` records the measurements behind that choice, and the proposition re-checks its own coverage.

6. `phaseA_link_agrees`, with `PhaseA.run_steps_le` and `PhaseA.run_heap_le` of `Vego/PhaseAProofs.lean`.
   The model of the phase A matcher in `Vego/PhaseA.lean` is bounded for every program and subject by the heap and step figures the contract reports, by ordinary induction and a potential argument on the closure.
   The link theorem checks the model against the interpreted engine on the corpus, result, bytes and loop meter, and checks the engine's contract figures against the proven ones.
   `phaseA_run_steps_anchored`, from `PhaseA.run_steps_le_anchored` of `Vego/PhaseAAnchored.lean`, is the tighter step figure of a start-anchored program of bounded depth, which pays for `depth + 3` boundaries instead of one per byte.
   Its certificate is decidable, and the link theorem checks that every contract the engine reports with that figure carries one.

5. `spec_agrees_with_corpus`, `engine_meets_spec_on_corpus` and `engine_meets_spec_exhaustively`.
   The `Ere` library is a model of the ERE specification, written from its text.
   The first theorem says that the recorded output of every corpus command the specification constrains meets the specification's verdict, at a pinned coverage.
   The second composes it with the corpus theorem: on those commands, the interpreted engine meets the specification, with the Go engine out of the trust base.
   The third compares the interpreted engine with the specification directly, on every pattern of a small token language and every short subject, at a pinned coverage of over a million executions.
-/

import Vego.Probe
import Vego.Corpus
import Vego.SpecCheck
import Vego.Exhaustive
import Vego.PhaseALink
import Vego.PhaseAProofs
import Vego.PhaseAAnchored
import Vego.PhaseARun

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

theorem revera_corpus_agrees_within_contract : corpusAgrees () = true := by
  native_decide

/--
The specification agrees with the reference output of every constrained corpus command, at the coverage
`expectedCoverage` states: every compile of a defined pattern, and every execution of one on a subject in
the interface domain that the enumeration could afford.
-/
theorem spec_agrees_with_corpus : specCorpusAgrees () = true := by
  native_decide

/--
The interpreted engine meets the specification on an exhaustive small domain, directly and without any
recorded output in between: every pattern of up to four tokens on every subject over `{a, b}` of up to
three characters under two flag settings, and every pattern of up to three tokens under all sixteen
compile flag combinations and all four execution flag combinations on subjects with newlines and
uppercase letters. `coverageStructure` and `coverageFlags` pin the sizes.
-/
theorem engine_meets_spec_exhaustively : exhaustiveAgrees () = true := by
  native_decide

/--
The phase A model against the interpreted engine, on every corpus execution that runs phase A alone.
The model reproduces the engine's match result, allocates exactly the bytes the engine allocates, and its
step figure dominates the engine's loop meter. On every `T` command of such a pattern, the heap and step
figures the engine reports are the figures `Vego/PhaseAProofs.lean` proves for the model.
`linkCoverage` pins how many executions and contract queries that is.

With `PhaseA.run_steps_le` and `PhaseA.run_heap_le`, which quantify over every program, atom test and
subject, this closes the chain from the shipped contract to a proof: the figures `ContractFor` computes for
phase A are the proven bounds of the algorithm the corpus shows the engine to be running.
-/
theorem phaseA_link_agrees : PhaseA.linkAgrees () = true := by
  native_decide

/--
Phase A is functionally correct for every program and subject. `PhaseACorrect.lean` states the reference: a
thread of the program at a boundary of the subject with the start and counters it carries, its epsilon and
consuming steps, the candidates it accepts, and the selection order of earliest start, smallest counters,
longest end. The model's `(matched, so, eo)` is that selection: when it reports a match, its start and end with
the counters it kept form a candidate that the order puts at or before every candidate; when it reports none,
there is no candidate. The hypotheses are the program well-formedness `Prog.wfCheck` decides, the fit of the
multi-character probes in the ring, and `ScanSound`, the soundness of the scan filter against the reference.
-/
theorem phaseA_run_correct (p : PhaseA.Prog) (atoms : PhaseA.Atoms) (input : PhaseA.Input)
    (hwf : p.wfCheck = true) (hawf : atoms.wf2 p) (hring : 2 ≤ p.ring) (hscan : PhaseA.ScanSound p atoms input) :
    ((PhaseA.run p atoms input).matched = true →
      ∃ c, PhaseA.IsBest p atoms input (PhaseA.run p atoms input).so c (PhaseA.run p atoms input).eo) ∧
    ((PhaseA.run p atoms input).matched = false → ∀ s c e, ¬ PhaseA.Cand p atoms input s c e) :=
  PhaseA.run_correct p atoms input (PhaseA.wfCheck_sound p hwf).1 hawf hring hscan

/--
Phase A of a start-anchored program of bounded depth pays for `depth + 3` boundaries rather than one per
byte, for every atom test and every subject without newline mode. `anchoredCheck` decides the certificate:
labels that never drop along a split or jump edge, grow by one across a consuming instruction and stay
within the depth, and the seed reach, which holds the start, is closed under those edges and holds no
consuming instruction. The engine computes the depth at compile time, and `phaseA_link_agrees` checks that
every contract it reports with the anchored figure carries such a certificate.
-/
theorem phaseA_run_steps_anchored (p : PhaseA.Prog) (atoms : PhaseA.Atoms) (input : PhaseA.Input)
    (atom : Nat) (d : Array Nat) (seed : Array Bool) (depth : Nat) (hwf : p.wfCheck = true)
    (hawf : atoms.wf p) (hnl : input.nlMode = false) (hcert : PhaseA.anchoredCheck p d seed depth = true) :
    PhaseA.stepFigure (PhaseA.run p atoms input).m p.k p.ring atom ≤
      PhaseA.stepsFigureAnchored p atom input.bytes.size depth :=
  have ha := PhaseA.anchoredCheck_sound p d seed depth hcert
  PhaseA.run_steps_le_anchored p atoms input atom _ _ depth (PhaseA.wfCheck_sound p hwf).1 hawf
    ((PhaseA.wfCheck_sound p hwf).2 ha.enabled) hnl ha

/--
The two corpus theorems composed: on every corpus command the specification constrains, the interpreted
revera engine produces an output that meets the specification's verdict.

The Go engine is not in the trust base of this statement.
Its recorded outputs are the bridge between the two native evaluations, and both sides are checked
against them, so the equality holds whether or not the recording was right.
-/
theorem engine_meets_spec_on_corpus :
    ∀ pairs tp, corpusPairs = .ok pairs → reveraChecked = .ok tp →
      ∃ lines, replayLines tp (sensiblePairs pairs) = .ok lines ∧
        ∀ (i : Nat) (v : Verdict),
          (verdicts specBudget ((sensiblePairs pairs).map (·.1))).1[i]? = some (some v) →
          ∃ l, lines[i]? = some l ∧ v.holds l = true := by
  intro pairs tp hp ht
  have hc := revera_corpus_agrees_within_contract
  have hs := spec_agrees_with_corpus
  unfold corpusAgrees at hc
  unfold specCorpusAgrees at hs
  rw [hp] at hc hs
  simp only [Bool.and_eq_true] at hc hs
  obtain ⟨-, hreplay⟩ := hc
  obtain ⟨hhold, -⟩ := hs
  unfold replayAgrees at hreplay
  rw [ht] at hreplay
  simp only at hreplay
  revert hreplay
  cases hl : replayLines tp (sensiblePairs pairs) with
  | error e => intro h; simp at h
  | ok lines =>
    intro h
    simp only [Bool.and_eq_true, beq_iff_eq] at h
    refine ⟨lines, rfl, ?_⟩
    intro i v hv
    rw [h.1]
    have hidx := verdictsHold_index _ _ hhold i v
    simp only [List.getElem?_map]
    cases hq : (sensiblePairs pairs)[i]? with
    | none =>
      exfalso
      have hlen := verdicts_length specBudget ((sensiblePairs pairs).map (·.1))
      have h1 := (List.getElem?_eq_some_iff.mp hv).1
      have h2 := List.getElem?_eq_none_iff.mp hq
      simp only [List.length_map] at hlen
      omega
    | some q =>
      exact ⟨q.2, rfl, hidx q hv hq⟩

end Vego
