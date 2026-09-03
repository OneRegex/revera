/-
The specification applied to the corpus.

`verdicts` walks the corpus commands in order, tracks the locale and the current pattern like the driver
session does, and states for every command what the specification requires of its output, when it
requires anything. It reads only the commands, never the expected outputs, so a verdict is a function of
the command stream alone.

A command is left unconstrained in exactly these cases, and every one is counted:

- the selected locale is not POSIX, where sections 7.7 and 14.3 leave ranges and multi-character lists
  to the implementation;
- the pattern is a free spelling (section 14.2 or 14.3), or lies outside the interface domain;
- the subject holds a NUL or is not valid UTF-8, so it is not a string of the interface (section 3.1);
- the enumeration of derivations ran past its budget, a limit of the checker and not of the specification;
- the command is not a compile or an execution.
-/

import Vego.CorpusData
import Ere.Semantics

namespace Vego

open Ere

/-- What the specification requires of one output line. -/
inductive Verdict where
  /-- The output must be exactly this line. -/
  | exact (line : String)
  /-- The compile must fail: a nonzero code in `C <code> <pos> 0`. -/
  | compileFails
  deriving Repr, BEq, DecidableEq

def Verdict.holds : Verdict → String → Bool
  | .exact l, got => got == l
  | .compileFails, got =>
    match (got.splitOn " ").filter (· ≠ "") with
    | ["C", code, _, _] => code != "0"
    | _ => false

/-- The coverage figures of one walk. -/
structure Coverage where
  definedC : Nat := 0
  invalidC : Nat := 0
  freeC : Nat := 0
  nonPosixC : Nat := 0
  execChecked : Nat := 0
  execOutsideDomain : Nat := 0
  execOverBudget : Nat := 0
  execUnconstrained : Nat := 0
  deriving Repr, BEq, DecidableEq, Inhabited

/-- The walk state: the locale in force and the pattern the specification is currently talking about. -/
structure Walk where
  posix : Bool := true
  cur : Option (Ere × Nat × CFlags) := none
  cov : Coverage := {}

/-- Render an outcome as the driver's `X` line. -/
def renderOutcome : Outcome → String
  | .nomatch => "X 0 0"
  | .matched pairs => "X 0 1" ++ String.join (pairs.map fun (so, eo) => s!" {so},{eo}")

/-- The verdict of one command, and the walk state after it. -/
def step (budget : Nat) (w : Walk) (cmd : String) : Option Verdict × Walk :=
  match (cmd.splitOn " ").filter (· ≠ "") with
  | "P" :: _ => (none, { w with posix := true })
  | "L" :: _ => (none, { w with posix := false, cur := none })
  | ["C", flagsT, patT] =>
    match flagsT.toNat?, tokDecode patT with
    | some flagBits, some pat =>
      if !w.posix then
        (none, { w with cur := none, cov := { w.cov with nonPosixC := w.cov.nonPosixC + 1 } })
      else
        let flags := CFlags.ofBits flagBits
        match parsePattern posixLocale flags pat with
        | .defined e nsub =>
          (some (.exact s!"C 0 0 {nsub}"),
           { w with cur := some (e, nsub, flags),
                    cov := { w.cov with definedC := w.cov.definedC + 1 } })
        | .invalid =>
          (some .compileFails,
           { w with cur := none, cov := { w.cov with invalidC := w.cov.invalidC + 1 } })
        | .free =>
          (none, { w with cur := none, cov := { w.cov with freeC := w.cov.freeC + 1 } })
    | _, _ => (none, { w with cur := none })
  | ["X", eflagsT, subjT] =>
    match w.cur, eflagsT.toNat?, tokDecode subjT with
    | some (e, nsub, flags), some eflagBits, some subjBytes =>
      match Subject.ofBytes subjBytes with
      | none =>
        (none, { w with cov := { w.cov with execOutsideDomain := w.cov.execOutsideDomain + 1 } })
      | some subj =>
        let ctx : Ere.Ctx := { loc := posixLocale, flags, eflags := EFlags.ofBits eflagBits, subj }
        match exec ctx e nsub budget with
        | none =>
          (none, { w with cov := { w.cov with execOverBudget := w.cov.execOverBudget + 1 } })
        | some out =>
          (some (.exact (renderOutcome out)),
           { w with cov := { w.cov with execChecked := w.cov.execChecked + 1 } })
    | _, _, _ =>
      (none, { w with cov := { w.cov with execUnconstrained := w.cov.execUnconstrained + 1 } })
  | _ => (none, w)

/-- The verdict of every command from a walk state on, in order, with the final coverage. -/
def verdictsFrom (budget : Nat) (w : Walk) : List String → List (Option Verdict) × Coverage
  | [] => ([], w.cov)
  | cmd :: cmds =>
    let r := step budget w cmd
    let rest := verdictsFrom budget r.2 cmds
    (r.1 :: rest.1, rest.2)

/-- The verdict of every command, in order, with the final coverage. -/
def verdicts (budget : Nat) (cmds : List String) : List (Option Verdict) × Coverage :=
  verdictsFrom budget {} cmds

theorem verdictsFrom_length (budget : Nat) (w : Walk) (cmds : List String) :
    (verdictsFrom budget w cmds).1.length = cmds.length := by
  induction cmds generalizing w with
  | nil => rfl
  | cons c cs ih => simp [verdictsFrom, ih]

theorem verdicts_length (budget : Nat) (cmds : List String) :
    (verdicts budget cmds).1.length = cmds.length := verdictsFrom_length budget {} cmds

/-- The work budget of one execution, in enumeration steps. -/
def specBudget : Nat := 2000000

/-- Every constrained command of `pairs` has an expected output that meets its verdict. -/
def verdictsHold : List (Option Verdict) → List (String × String) → Bool
  | some v :: vs, (_, want) :: ps => v.holds want && verdictsHold vs ps
  | none :: vs, _ :: ps => verdictsHold vs ps
  | [], [] => true
  | _, _ => false

theorem verdictsHold_index (vs : List (Option Verdict)) (ps : List (String × String))
    (h : verdictsHold vs ps = true) :
    ∀ (i : Nat) (v : Verdict) (p : String × String),
      vs[i]? = some (some v) → ps[i]? = some p → v.holds p.2 = true := by
  induction vs generalizing ps with
  | nil => intro i v p hv; simp at hv
  | cons o vs ih =>
    intro i v p hv hp
    cases ps with
    | nil => cases o <;> simp [verdictsHold] at h
    | cons q ps =>
      cases i with
      | zero =>
        simp at hv hp
        subst hv hp
        simp [verdictsHold] at h
        exact h.1
      | succ i =>
        simp at hv hp
        cases o with
        | none => exact ih ps (by simpa [verdictsHold] using h) i v p hv hp
        | some v' =>
          simp [verdictsHold] at h
          exact ih ps h.2 i v p hv hp

/-- The commands whose expected output violates the verdict, for diagnosis. -/
def mismatches (vs : List (Option Verdict)) (pairs : List (String × String)) :
    List (Nat × String × String × Verdict) :=
  ((vs.zip pairs).zipIdx.filterMap fun ((v, (cmd, want)), idx) =>
    match v with
    | some v => if v.holds want then none else some (idx, cmd, want, v)
    | none => none)

/--
The coverage the theorem pins down.
These figures are what the walk over the replayed corpus produces; the theorem fails if any of them moves,
so a change in coverage is a change in the statement.
-/
def expectedCoverage : Coverage :=
  { definedC := 9653, invalidC := 0, freeC := 66, nonPosixC := 148,
    execChecked := 66576, execOutsideDomain := 1512, execOverBudget := 0,
    execUnconstrained := 7508 }

/--
The specification agrees with the reference output of every constrained corpus command.
The `Unit` argument keeps this from being a closed term, which the module would otherwise evaluate on load.
-/
def specCorpusAgrees (_ : Unit) : Bool :=
  match corpusPairs with
  | .error _ => false
  | .ok pairs =>
    let sensible := sensiblePairs pairs
    let (vs, cov) := verdicts specBudget (sensible.map (·.1))
    verdictsHold vs sensible && cov == expectedCoverage

end Vego
