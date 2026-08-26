/-
Soundness of the resource meter, universally.

The driver measures one Exec by resetting the meter, running the
call, and reading the counters. That methodology is only sound if
no interpreter step can shrink a counter, and if the call-depth
counter returns to its entry value whenever a call completes, so
the recorded maximum is the true peak of the run. This file proves
both, for every function of the interpreter, by one induction on
fuel. Nothing here uses native evaluation.

`MeterOK h h'` is the per-step contract: the allocation, step and
loop counters never decrease, the depth is balanced, and the
recorded maximum never decreases. `MOK x` lifts it to a whole
computation: every successful run of x satisfies it. The final
corollary lifts it to harness calls, which is the form the driver
session relies on.
-/

import Vego.Machine
import Vego.CostLemmas

namespace Vego

set_option maxHeartbeats 2000000

/-- The two heaps agree on every meter field. -/
def sameMeter (h h' : Heap) : Prop :=
  h'.allocBytes = h.allocBytes ∧ h'.steps = h.steps ∧
  h'.loops = h.loops ∧ h'.depth = h.depth ∧ h'.maxDepth = h.maxDepth

/-- What one completed interpreter action guarantees: counters
grow monotonically, and the depth is balanced. -/
def MeterOK (h h' : Heap) : Prop :=
  h.allocBytes ≤ h'.allocBytes ∧ h.steps ≤ h'.steps ∧
  h.loops ≤ h'.loops ∧ h'.depth = h.depth ∧ h.maxDepth ≤ h'.maxDepth

theorem sameMeter.meterOK {h h' : Heap} (s : sameMeter h h') :
    MeterOK h h' := by
  obtain ⟨a, b, c, d, e⟩ := s
  exact ⟨by omega, by omega, by omega, d, by omega⟩

theorem MeterOK.refl (h : Heap) : MeterOK h h :=
  ⟨Nat.le_refl _, Nat.le_refl _, Nat.le_refl _, rfl, Nat.le_refl _⟩

theorem MeterOK.trans {h1 h2 h3 : Heap} (a : MeterOK h1 h2)
    (b : MeterOK h2 h3) : MeterOK h1 h3 := by
  obtain ⟨a1, a2, a3, a4, a5⟩ := a
  obtain ⟨b1, b2, b3, b4, b5⟩ := b
  exact ⟨by omega, by omega, by omega, by omega, by omega⟩

/-- Every successful run of the computation satisfies MeterOK. -/
def MOK {α : Type} (x : M α) : Prop :=
  ∀ h r h', x h = .ok r h' → MeterOK h h'

theorem MOK_pure {α : Type} (a : α) : MOK (pure a : M α) := by
  intro h r h' e
  cases e
  exact MeterOK.refl h

theorem MOK_trap {α : Type} (t : Trap) : MOK (M.trap t : M α) := by
  intro h r h' e
  cases e

theorem MOK_bind {α β : Type} {x : M α} {f : α → M β}
    (hx : MOK x) (hf : ∀ a, MOK (f a)) : MOK (x >>= f) := by
  intro h r h' e
  simp only [M.bind_def] at e
  split at e
  next v h1 heq => exact (hx h v h1 heq).trans (hf v h1 r h' e)
  next => cases e

/-- Heap primitives that only touch cells keep the meter intact. -/
theorem MOK_of_sameMeter {α : Type} {x : M α}
    (hx : ∀ h r h', x h = .ok r h' → sameMeter h h') : MOK x :=
  fun h r h' e => (hx h r h' e).meterOK

theorem MOK_alloc (v : Val) : MOK (M.alloc v) := by
  apply MOK_of_sameMeter
  intro h r h' e
  unfold M.alloc at e
  split at e
  · split at e
    · cases e; exact ⟨rfl, rfl, rfl, rfl, rfl⟩
    · cases e
  · cases e; exact ⟨rfl, rfl, rfl, rfl, rfl⟩

theorem MOK_freeCell (id : Nat) : MOK (M.freeCell id) := by
  apply MOK_of_sameMeter
  intro h r h' e
  unfold M.freeCell at e
  split at e
  · cases e; exact ⟨rfl, rfl, rfl, rfl, rfl⟩
  · cases e

theorem MOK_readCell (i : Nat) : MOK (M.readCell i) := by
  apply MOK_of_sameMeter
  intro h r h' e
  unfold M.readCell at e
  split at e
  · cases e; exact ⟨rfl, rfl, rfl, rfl, rfl⟩
  · cases e

theorem MOK_cellGen (i : Nat) : MOK (M.cellGen i) := by
  apply MOK_of_sameMeter
  intro h r h' e
  unfold M.cellGen at e
  split at e
  · cases e; exact ⟨rfl, rfl, rfl, rfl, rfl⟩
  · cases e

theorem MOK_writeCell (i : Nat) (v : Val) : MOK (M.writeCell i v) := by
  apply MOK_of_sameMeter
  intro h r h' e
  unfold M.writeCell at e
  split at e
  · cases e; exact ⟨rfl, rfl, rfl, rfl, rfl⟩
  · cases e

theorem MOK_readLoc (obj gen : Nat) (path : Path) :
    MOK (M.readLoc obj gen path) := by
  apply MOK_of_sameMeter
  intro h r h' e
  unfold M.readLoc at e
  split at e
  · split at e
    · cases e
    · split at e
      · cases e; exact ⟨rfl, rfl, rfl, rfl, rfl⟩
      · cases e
  · cases e

theorem MOK_writeLoc (obj gen : Nat) (path : Path) (nv : Val) :
    MOK (M.writeLoc obj gen path nv) := by
  apply MOK_of_sameMeter
  intro h r h' e
  unfold M.writeLoc at e
  split at e
  · cases e
  · split at e
    · cases e
    · split at e <;> first
        | (cases e; exact ⟨rfl, rfl, rfl, rfl, rfl⟩)
        | ((split at e <;> try split at e) <;> (try cases e) <;>
            exact ⟨rfl, rfl, rfl, rfl, rfl⟩)

/-- Ticks and charges only grow their counters. -/
theorem MOK_tickStmt : MOK M.tickStmt := by
  intro h r h' e
  cases e
  exact ⟨Nat.le_refl _, Nat.le_succ _, Nat.le_refl _, rfl,
         Nat.le_refl _⟩

theorem MOK_tickLoop : MOK M.tickLoop := by
  intro h r h' e
  cases e
  exact ⟨Nat.le_refl _, Nat.le_succ _, Nat.le_succ _, rfl,
         Nat.le_refl _⟩

theorem MOK_charge (b : Nat) : MOK (M.charge b) := by
  intro h r h' e
  cases e
  exact ⟨Nat.le_add_right _ _, Nat.le_refl _, Nat.le_refl _, rfl,
         Nat.le_refl _⟩

/-- The expect helpers are pure or trap. -/
theorem MOK_expectInt (v : Val) : MOK (M.expectInt v) := by
  unfold M.expectInt
  split
  · exact MOK_pure _
  · exact MOK_trap _

theorem MOK_expectBool (v : Val) : MOK (M.expectBool v) := by
  unfold M.expectBool
  split
  · exact MOK_pure _
  · exact MOK_trap _

theorem MOK_expectStr (v : Val) : MOK (M.expectStr v) := by
  unfold M.expectStr
  split
  · exact MOK_pure _
  · exact MOK_trap _

theorem MOK_expectSlice (v : Val) : MOK (M.expectSlice v) := by
  unfold M.expectSlice
  split
  · exact MOK_pure _
  · exact MOK_trap _

theorem MOK_readElem (base : Option Loc) (off k : Nat) :
    MOK (M.readElem base off k) := by
  unfold M.readElem
  split
  · exact MOK_trap _
  · apply MOK_bind (MOK_readLoc _ _ _)
    intro a
    split
    · split
      · exact MOK_pure _
      · exact MOK_trap _
    · exact MOK_trap _

theorem MOK_writeElem (base : Option Loc) (off k : Nat) (nv : Val) :
    MOK (M.writeElem base off k nv) := by
  unfold M.writeElem
  split
  · exact MOK_trap _
  · exact MOK_writeLoc _ _ _ _

theorem MOK_writeElems (base : Option Loc) (off k : Nat)
    (vs : Array Val) : MOK (M.writeElems base off k vs) := by
  unfold M.writeElems
  split
  · exact MOK_pure _
  · split
    · exact MOK_trap _
    · apply MOK_bind (MOK_readLoc _ _ _)
      intro a
      split
      · split
        · exact MOK_trap _
        · exact MOK_writeLoc _ _ _ _
      · exact MOK_trap _

theorem MOK_readElems (base : Option Loc) (off len : Nat) :
    MOK (M.readElems base off len) := by
  unfold M.readElems
  split
  · exact MOK_pure _
  · split
    · exact MOK_trap _
    · apply MOK_bind (MOK_readLoc _ _ _)
      intro a
      split
      · split
        · exact MOK_trap _
        · exact MOK_pure _
      · exact MOK_trap _

theorem MOK_gatherSrc (sv : Val) (srcIsStr : Bool) :
    MOK (M.gatherSrc sv srcIsStr) := by
  unfold M.gatherSrc
  split
  · apply MOK_bind (MOK_expectStr _)
    intro a
    exact MOK_pure _
  · apply MOK_bind (MOK_expectSlice _)
    intro a
    exact MOK_readElems _ _ _

theorem MOK_bindSlot (fr : Frame) (slot : Nat) (v : Val) :
    MOK (M.bindSlot fr slot v) := by
  unfold M.bindSlot
  apply MOK_bind (MOK_alloc _)
  intro a
  obtain ⟨cell, g⟩ := a
  exact MOK_pure _

theorem MOK_rebindSlot (fr : Frame) (slot : Nat) (v : Val) :
    MOK (M.rebindSlot fr slot v) := by
  unfold M.rebindSlot
  split
  · split
    · exact MOK_bind (MOK_freeCell _) (fun _ => MOK_bindSlot fr slot v)
    · exact MOK_bindSlot fr slot v
  · exact MOK_bindSlot fr slot v

theorem MOK_freeFrameFrom (fr : Frame) (i : Nat) :
    MOK (M.freeFrameFrom fr i) := by
  unfold M.freeFrameFrom
  split
  · split
    · exact MOK_bind (MOK_freeCell _)
        (fun _ => MOK_freeFrameFrom fr (i + 1))
    · exact MOK_freeFrameFrom fr (i + 1)
  · exact MOK_pure _
termination_by fr.size - i

theorem MOK_freeFrame (fr : Frame) : MOK (M.freeFrame fr) :=
  MOK_freeFrameFrom fr 0

theorem MOK_allocLoopCell (fr : Frame) (slot : Option Nat) :
    MOK (allocLoopCell fr slot) := by
  unfold allocLoopCell
  split
  · exact MOK_pure _
  · apply MOK_bind (MOK_rebindSlot _ _ _)
    intro a
    exact MOK_pure _

/-- The allocation forms grow the meter and keep it balanced. -/
theorem MOK_doAppend (c : Ctx) (sv : Val) (adds : Array Val)
    (elemTy : VTy) : MOK (doAppend c sv adds elemTy) := by
  intro h r h' e
  cases sv with
  | slice base off len cap =>
    unfold doAppend at e
    simp only [M.expectSlice, M.bind_def, M.pure_def] at e
    split at e
    · cases e
      exact MeterOK.refl h
    · split at e
      · simp only [M.bind_def] at e
        split at e
        next u h1 hwe =>
          cases e
          exact MOK_writeElems _ _ _ _ h u h' hwe
        next => cases e
      · simp only [M.bind_def] at e
        split at e
        next u h1 hch =>
          split at e
          next live h2 hre =>
            split at e
            next cg h3 hal =>
              cases e
              exact ((MOK_charge _ h u h1 hch).trans
                (MOK_readElems _ _ _ h1 live h2 hre)).trans
                (MOK_alloc _ h2 cg h' hal)
            next => cases e
          next => cases e
        next => cases e
  | i v => simp only [doAppend, M.expectSlice, M.bind_def] at e; cases e
  | b v => simp only [doAppend, M.expectSlice, M.bind_def] at e; cases e
  | s v => simp only [doAppend, M.expectSlice, M.bind_def] at e; cases e
  | arr es => simp only [doAppend, M.expectSlice, M.bind_def] at e; cases e
  | strukt fs =>
    simp only [doAppend, M.expectSlice, M.bind_def] at e; cases e
  | ptr o g p =>
    simp only [doAppend, M.expectSlice, M.bind_def] at e; cases e

theorem MOK_doMake (c : Ctx) (elemTy : VTy) (n cp : Int) :
    MOK (doMake c elemTy n cp) := by
  unfold doMake
  split
  · exact MOK_trap _
  · apply MOK_bind (MOK_charge _)
    intro a
    apply MOK_bind (MOK_alloc _)
    intro cg
    obtain ⟨cell, g⟩ := cg
    exact MOK_pure _

theorem MOK_doSliceLit (c : Ctx) (elemTy : VTy) (vs : Array Val) :
    MOK (doSliceLit c elemTy vs) := by
  unfold doSliceLit
  apply MOK_bind (MOK_charge _)
  intro a
  apply MOK_bind (MOK_alloc _)
  intro cg
  obtain ⟨cell, g⟩ := cg
  exact MOK_pure _

theorem MOK_doStrToBytes (s : ByteArray) : MOK (doStrToBytes s) := by
  unfold doStrToBytes
  apply MOK_bind (MOK_charge _)
  intro a
  apply MOK_bind (MOK_alloc _)
  intro cg
  obtain ⟨cell, g⟩ := cg
  exact MOK_pure _

theorem MOK_doBytesToStr (vs : Array Val) : MOK (doBytesToStr vs) := by
  unfold doBytesToStr
  apply MOK_bind (MOK_charge _)
  intro a
  split
  · exact MOK_pure _
  · exact MOK_trap _

/-- forIn over a list keeps MeterOK when every body step does. -/
theorem MOK_forIn_list {α β : Type} (xs : List α) (b : β)
    (f : α → β → M (ForInStep β)) (hf : ∀ x b, MOK (f x b)) :
    MOK (forIn xs b f) := by
  induction xs generalizing b with
  | nil => simpa [List.forIn_nil] using MOK_pure b
  | cons x rest ih =>
    rw [List.forIn_cons]
    apply MOK_bind (hf x b)
    intro s
    cases s with
    | done b' => exact MOK_pure _
    | yield b' => exact ih b'

/-! ## The interpreter, whole

One mutual induction on fuel: every function of the interpreter
satisfies MeterOK on success. Counters never shrink and the call
depth is balanced, so the driver's reset-run-read measurement of
one Exec is sound. -/

mutual

theorem MOK_evalPlace (fuel : Nat) (c : Ctx) (fr : Frame)
    (p : TPlace) : MOK (evalPlace fuel c fr p) := by
  cases p with
  | localP slot =>
    cases fuel with
    | zero => simp only [evalPlace]; exact MOK_trap _
    | succ fuel =>
      simp only [evalPlace]
      split
      · split
        · exact MOK_trap _
        · exact MOK_bind (MOK_cellGen _) (fun g => MOK_pure _)
      · exact MOK_trap _
  | fieldP base i viaPtr =>
    cases fuel with
    | zero => simp only [evalPlace]; exact MOK_trap _
    | succ fuel =>
      simp only [evalPlace]
      apply MOK_bind (MOK_evalPlace fuel c fr base)
      intro a
      obtain ⟨obj, g, path⟩ := a
      simp only []
      split
      · apply MOK_bind (MOK_readLoc _ _ _)
        intro v
        split
        · exact MOK_pure _
        · exact MOK_trap _
      · exact MOK_pure _
  | indexArrP base n idx =>
    cases fuel with
    | zero => simp only [evalPlace]; exact MOK_trap _
    | succ fuel =>
      simp only [evalPlace]
      apply MOK_bind (MOK_evalPlace fuel c fr base)
      intro a
      obtain ⟨obj, g, path⟩ := a
      simp only []
      apply MOK_bind (MOK_evalExpr fuel c fr idx)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro iv
      split
      · exact MOK_trap _
      · exact MOK_pure _
  | indexSliceP sliceVal idx =>
    cases fuel with
    | zero => simp only [evalPlace]; exact MOK_trap _
    | succ fuel =>
      simp only [evalPlace]
      apply MOK_bind (MOK_evalExpr fuel c fr sliceVal)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨base, off, len, cap⟩ := a
      simp only []
      apply MOK_bind (MOK_evalExpr fuel c fr idx)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro iv
      split
      · exact MOK_trap _
      · split
        · exact MOK_pure _
        · exact MOK_trap _

theorem MOK_evalExprs (fuel : Nat) (c : Ctx) (fr : Frame)
    (es : List TExpr) : MOK (evalExprs fuel c fr es) := by
  cases es with
  | nil => simp only [evalExprs]; exact MOK_pure _
  | cons e rest =>
    cases fuel with
    | zero => simp only [evalExprs]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExprs]
      apply MOK_bind (MOK_evalExpr fuel c fr e)
      intro v
      apply MOK_bind (MOK_evalExprs fuel c fr rest)
      intro vs
      exact MOK_pure _

theorem MOK_evalExpr (fuel : Nat) (c : Ctx) (fr : Frame)
    (ex : TExpr) : MOK (evalExpr fuel c fr ex) := by
  cases ex with
  | litInt v => simp only [evalExpr]; exact MOK_pure _
  | litBool v => simp only [evalExpr]; exact MOK_pure _
  | litStr v => simp only [evalExpr]; exact MOK_pure _
  | zeroOf ty => simp only [evalExpr]; exact MOK_pure _
  | globalGet idx =>
    simp only [evalExpr]
    split
    · exact MOK_pure _
    · exact MOK_trap _
  | placeGet p =>
    cases p with
    | localP slot =>
      cases fuel with
      | zero => simp only [evalExpr]; exact MOK_trap _
      | succ fuel =>
        simp only [evalExpr]
        split
        · split
          · exact MOK_trap _
          · exact MOK_readCell _
        · exact MOK_trap _
    | fieldP base i viaPtr =>
      cases fuel with
      | zero => simp only [evalExpr]; exact MOK_trap _
      | succ fuel =>
        simp only [evalExpr]
        apply MOK_bind (MOK_evalPlace fuel c fr _)
        intro a
        obtain ⟨obj, g, path⟩ := a
        simp only []
        exact MOK_readLoc _ _ _
    | indexArrP base n idx =>
      cases fuel with
      | zero => simp only [evalExpr]; exact MOK_trap _
      | succ fuel =>
        simp only [evalExpr]
        apply MOK_bind (MOK_evalPlace fuel c fr _)
        intro a
        obtain ⟨obj, g, path⟩ := a
        simp only []
        exact MOK_readLoc _ _ _
    | indexSliceP sliceVal idx =>
      cases fuel with
      | zero => simp only [evalExpr]; exact MOK_trap _
      | succ fuel =>
        simp only [evalExpr]
        apply MOK_bind (MOK_evalPlace fuel c fr _)
        intro a
        obtain ⟨obj, g, path⟩ := a
        simp only []
        exact MOK_readLoc _ _ _
  | fieldGet x i =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro v
      split
      · split
        · exact MOK_pure _
        · exact MOK_trap _
      · apply MOK_bind (MOK_readLoc _ _ _)
        intro w
        split
        · split
          · exact MOK_pure _
          · exact MOK_trap _
        · exact MOK_trap _
      · exact MOK_trap _
  | indexSliceGet x idx =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨base, off, len, cap⟩ := a
      simp only []
      apply MOK_bind (MOK_evalExpr fuel c fr idx)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro iv
      split
      · exact MOK_trap _
      · exact MOK_readElem _ _ _
  | indexStrGet x idx =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectStr __dl)
      intro s
      apply MOK_bind (MOK_evalExpr fuel c fr idx)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro iv
      split
      · exact MOK_trap _
      · exact MOK_pure _
  | indexArrVal x idx =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro xv
      apply MOK_bind (MOK_evalExpr fuel c fr idx)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro iv
      split
      · split
        · exact MOK_trap _
        · exact MOK_pure _
      · exact MOK_trap _
  | sliceOfSlice x lo hi =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨base, off, len, cap⟩ := a
      simp only []
      cases lo with
      | some le =>
        apply MOK_bind (MOK_evalExpr fuel c fr le)
        intro d1
        apply MOK_bind (MOK_expectInt d1)
        intro lov
        cases hi with
        | some he =>
          apply MOK_bind (MOK_evalExpr fuel c fr he)
          intro d2
          apply MOK_bind (MOK_expectInt d2)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
        | none =>
          apply MOK_bind (MOK_pure _)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
      | none =>
        apply MOK_bind (MOK_pure _)
        intro lov
        cases hi with
        | some he =>
          apply MOK_bind (MOK_evalExpr fuel c fr he)
          intro d2
          apply MOK_bind (MOK_expectInt d2)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
        | none =>
          apply MOK_bind (MOK_pure _)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
  | sliceOfArr basep n lo hi =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalPlace fuel c fr basep)
      intro a
      obtain ⟨obj, g, path⟩ := a
      simp only []
      cases lo with
      | some le =>
        apply MOK_bind (MOK_evalExpr fuel c fr le)
        intro d1
        apply MOK_bind (MOK_expectInt d1)
        intro lov
        cases hi with
        | some he =>
          apply MOK_bind (MOK_evalExpr fuel c fr he)
          intro d2
          apply MOK_bind (MOK_expectInt d2)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
        | none =>
          apply MOK_bind (MOK_pure _)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
      | none =>
        apply MOK_bind (MOK_pure _)
        intro lov
        cases hi with
        | some he =>
          apply MOK_bind (MOK_evalExpr fuel c fr he)
          intro d2
          apply MOK_bind (MOK_expectInt d2)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
        | none =>
          apply MOK_bind (MOK_pure _)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
  | sliceOfStr x lo hi =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectStr __dl)
      intro s
      cases lo with
      | some le =>
        apply MOK_bind (MOK_evalExpr fuel c fr le)
        intro d1
        apply MOK_bind (MOK_expectInt d1)
        intro lov
        cases hi with
        | some he =>
          apply MOK_bind (MOK_evalExpr fuel c fr he)
          intro d2
          apply MOK_bind (MOK_expectInt d2)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
        | none =>
          apply MOK_bind (MOK_pure _)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
      | none =>
        apply MOK_bind (MOK_pure _)
        intro lov
        cases hi with
        | some he =>
          apply MOK_bind (MOK_evalExpr fuel c fr he)
          intro d2
          apply MOK_bind (MOK_expectInt d2)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
        | none =>
          apply MOK_bind (MOK_pure _)
          intro hiv
          split
          · exact MOK_trap _
          · exact MOK_pure _
  | callFn idx args =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalCall fuel c fr idx args)
      intro vs
      split
      · exact MOK_pure _
      · exact MOK_trap _
  | addrOf p =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalPlace fuel c fr p)
      intro a
      obtain ⟨obj, g, path⟩ := a
      simp only []
      exact MOK_pure _
  | arith op w x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a
      apply MOK_bind (MOK_evalExpr fuel c fr y)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro b
      split
      · exact MOK_pure _
      · exact MOK_trap _
  | shift left w x count =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a
      apply MOK_bind (MOK_evalExpr fuel c fr count)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro n
      split
      · exact MOK_pure _
      · exact MOK_trap _
  | icmp op x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a
      apply MOK_bind (MOK_evalExpr fuel c fr y)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro b
      exact MOK_pure _
  | scmp op x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectStr __dl)
      intro a
      apply MOK_bind (MOK_evalExpr fuel c fr y)
      intro __dl
      apply MOK_bind (MOK_expectStr __dl)
      intro b
      exact MOK_pure _
  | beq ne x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectBool __dl)
      intro a
      apply MOK_bind (MOK_evalExpr fuel c fr y)
      intro __dl
      apply MOK_bind (MOK_expectBool __dl)
      intro b
      exact MOK_pure _
  | deepEq ne x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro a
      apply MOK_bind (MOK_evalExpr fuel c fr y)
      intro b
      split
      · exact MOK_pure _
      · exact MOK_trap _
  | nilChk ne x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨base, off, len, cap⟩ := a
      simp only []
      exact MOK_pure _
  | land x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectBool __dl)
      intro a
      split
      · apply MOK_bind (MOK_evalExpr fuel c fr y)
        intro __dl2
        apply MOK_bind (MOK_expectBool __dl2)
        intro b
        exact MOK_pure _
      · exact MOK_pure _
  | lor x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectBool __dl)
      intro a
      split
      · exact MOK_pure _
      · apply MOK_bind (MOK_evalExpr fuel c fr y)
        intro __dl2
        apply MOK_bind (MOK_expectBool __dl2)
        intro b
        exact MOK_pure _
  | lnot x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectBool __dl)
      intro a
      exact MOK_pure _
  | negI w x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a
      exact MOK_pure _
  | bnotI w x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a
      exact MOK_pure _
  | convI w x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a
      exact MOK_pure _
  | strToBytes x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectStr __dl)
      intro s
      exact MOK_doStrToBytes s
  | bytesToStr x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨base, off, len, cap⟩ := a
      simp only []
      apply MOK_bind (MOK_readElems _ _ _)
      intro vs
      exact MOK_doBytesToStr vs
  | lenSlice x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨base, off, len, cap⟩ := a
      simp only []
      exact MOK_pure _
  | lenStr x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectStr __dl)
      intro s
      exact MOK_pure _
  | lenArr x n =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro v
      exact MOK_pure _
  | capSlice x =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨base, off, len, cap⟩ := a
      simp only []
      exact MOK_pure _
  | appendE s elems elemTy =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr s)
      intro sv
      apply MOK_bind (MOK_evalExprs fuel c fr elems)
      intro adds
      exact MOK_doAppend _ _ _ _
  | appendSpread s src srcIsStr elemTy =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr s)
      intro sv
      apply MOK_bind (MOK_evalExpr fuel c fr src)
      intro __dl
      apply MOK_bind (MOK_gatherSrc __dl srcIsStr)
      intro adds
      exact MOK_doAppend _ _ _ _
  | makeE elemTy len capE =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr len)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro n
      cases capE with
      | some ce =>
        apply MOK_bind (MOK_evalExpr fuel c fr ce)
        intro d1
        apply MOK_bind (MOK_expectInt d1)
        intro cp
        exact MOK_doMake _ _ _ _
      | none =>
        apply MOK_bind (MOK_pure _)
        intro cp
        exact MOK_doMake _ _ _ _
  | copyE dst src srcIsStr =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr dst)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨dbase, doff, dlen, dcap⟩ := a
      simp only []
      apply MOK_bind (MOK_evalExpr fuel c fr src)
      intro __dl
      apply MOK_bind (MOK_gatherSrc __dl srcIsStr)
      intro svals
      apply MOK_bind (MOK_writeElems _ _ _ _)
      intro u
      exact MOK_pure _
  | minE x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a
      apply MOK_bind (MOK_evalExpr fuel c fr y)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro b
      exact MOK_pure _
  | maxE x y =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExpr fuel c fr x)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a
      apply MOK_bind (MOK_evalExpr fuel c fr y)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro b
      exact MOK_pure _
  | mkStruct fields =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExprs fuel c fr fields)
      intro vs
      exact MOK_pure _
  | mkArr elems pad elemTy =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExprs fuel c fr elems)
      intro vs
      exact MOK_pure _
  | mkSliceLit elemTy elems =>
    cases fuel with
    | zero => simp only [evalExpr]; exact MOK_trap _
    | succ fuel =>
      simp only [evalExpr]
      apply MOK_bind (MOK_evalExprs fuel c fr elems)
      intro vs
      exact MOK_doSliceLit _ _ _

theorem MOK_runFn (fuel : Nat) (c : Ctx) (fn : TFunc)
    (avs : List Val) : MOK (runFn fuel c fn avs) := by
  intro h r h' e
  cases fuel with
  | zero => simp only [runFn] at e; cases e
  | succ fuel =>
    simp only [runFn, M.bind_def, M.pure_def] at e
    split at e
    next u1 h1 hent =>
      split at e
      next st h2 hfor =>
        split at e
        next fres h3 hstm =>
          split at e
          next u2 h4 hfree =>
            split at e
            next u3 h5 hexit =>
              have m2 : MeterOK h1 h2 := by
                refine MOK_forIn_list _ _ _ ?_ h1 st h2 hfor
                intro x b
                apply MOK_bind (MOK_alloc _)
                intro cg
                obtain ⟨cell, g⟩ := cg
                simp only []
                exact MOK_pure _
              have m3 : MeterOK h2 h3 :=
                MOK_execStmts fuel c _ _ h2 fres h3 hstm
              have m4 : MeterOK h3 h4 :=
                MOK_freeFrame _ h3 u2 h4 hfree
              cases hent
              cases hexit
              obtain ⟨e1, e2, e3, e4, e5⟩ := m2
              obtain ⟨f1, f2, f3, f4, f5⟩ := m3
              obtain ⟨g1, g2, g3, g4, g5⟩ := m4
              have e1' : h.allocBytes ≤ h2.allocBytes := e1
              have e2' : h.steps + 1 ≤ h2.steps := e2
              have e3' : h.loops + 1 ≤ h2.loops := e3
              have e4' : h2.depth = h.depth + 1 := e4
              have e5' : Nat.max h.maxDepth (h.depth + 1) ≤
                  h2.maxDepth := e5
              have e5'' : h.maxDepth ≤ h2.maxDepth :=
                Nat.le_trans (Nat.le_max_left _ _) e5'
              have final : MeterOK h
                  { h4 with depth := h4.depth - 1 } := by
                refine ⟨?_, ?_, ?_, ?_, ?_⟩
                · show h.allocBytes ≤ h4.allocBytes
                  omega
                · show h.steps ≤ h4.steps
                  omega
                · show h.loops ≤ h4.loops
                  omega
                · show h4.depth - 1 = h.depth
                  omega
                · show h.maxDepth ≤ h4.maxDepth
                  omega
              obtain ⟨flow, fr'⟩ := fres
              simp only [] at e
              cases flow with
              | retv vs =>
                cases e
                exact final
              | normal =>
                simp only [] at e
                split at e
                · cases e
                  exact final
                · cases e
              | brk => cases e
              | cont => cases e
            next => cases e
          next => cases e
        next => cases e
      next => cases e
    next => cases e

theorem MOK_evalCall (fuel : Nat) (c : Ctx) (fr : Frame) (idx : Nat)
    (args : List TExpr) : MOK (evalCall fuel c fr idx args) := by
  cases fuel with
  | zero => simp only [evalCall]; exact MOK_trap _
  | succ fuel =>
    simp only [evalCall]
    split
    · apply MOK_bind (MOK_evalExprs fuel c fr args)
      intro avs
      exact MOK_runFn fuel c _ avs
    · exact MOK_trap _

theorem MOK_execStmts (fuel : Nat) (c : Ctx) (fr : Frame)
    (ss : List TStmt) : MOK (execStmts fuel c fr ss) := by
  cases ss with
  | nil => simp only [execStmts]; exact MOK_pure _
  | cons s rest =>
    cases fuel with
    | zero => simp only [execStmts]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmts]
      apply MOK_bind MOK_tickStmt
      intro u
      apply MOK_bind (MOK_execStmt fuel c fr s)
      intro fres
      obtain ⟨f, fr'⟩ := fres
      simp only []
      cases f with
      | normal => exact MOK_execStmts fuel c fr' rest
      | brk => exact MOK_pure _
      | cont => exact MOK_pure _
      | retv vs => exact MOK_pure _

theorem MOK_execForLoop (fuel : Nat) (c : Ctx) (fr : Frame)
    (cond : Option TExpr) (post : Option TStmt) (body : List TStmt) :
    MOK (execForLoop fuel c fr cond post body) := by
  cases fuel with
  | zero => simp only [execForLoop]; exact MOK_trap _
  | succ fuel =>
    simp only [execForLoop]
    apply MOK_bind MOK_tickLoop
    intro u
    cases cond with
    | some ce =>
      apply MOK_bind (MOK_evalExpr fuel c fr ce)
      intro d
      apply MOK_bind (MOK_expectBool d)
      intro go
      split
      · exact MOK_pure _
      · apply MOK_bind (MOK_execStmts fuel c fr body)
        intro fres
        obtain ⟨f, fr2⟩ := fres
        simp only []
        cases f with
        | brk => exact MOK_pure _
        | retv vs => exact MOK_pure _
        | normal =>
          cases post with
          | some p =>
            apply MOK_bind (MOK_execStmt fuel c fr p)
            intro fres2
            exact MOK_execForLoop fuel c fr _ _ body
          | none => exact MOK_execForLoop fuel c fr _ _ body
        | cont =>
          cases post with
          | some p =>
            apply MOK_bind (MOK_execStmt fuel c fr p)
            intro fres2
            exact MOK_execForLoop fuel c fr _ _ body
          | none => exact MOK_execForLoop fuel c fr _ _ body
    | none =>
      apply MOK_bind (MOK_pure _)
      intro go
      split
      · exact MOK_pure _
      · apply MOK_bind (MOK_execStmts fuel c fr body)
        intro fres
        obtain ⟨f, fr2⟩ := fres
        simp only []
        cases f with
        | brk => exact MOK_pure _
        | retv vs => exact MOK_pure _
        | normal =>
          cases post with
          | some p =>
            apply MOK_bind (MOK_execStmt fuel c fr p)
            intro fres2
            exact MOK_execForLoop fuel c fr _ _ body
          | none => exact MOK_execForLoop fuel c fr _ _ body
        | cont =>
          cases post with
          | some p =>
            apply MOK_bind (MOK_execStmt fuel c fr p)
            intro fres2
            exact MOK_execForLoop fuel c fr _ _ body
          | none => exact MOK_execForLoop fuel c fr _ _ body

theorem MOK_execRangeLoop (fuel : Nat) (c : Ctx) (fr : Frame)
    (k n : Nat) (iCell : Option Nat) (vCell : Option Nat)
    (elemAt : Nat → M Val) (body : List TStmt)
    (helem : ∀ j, MOK (elemAt j)) :
    MOK (execRangeLoop fuel c fr k n iCell vCell elemAt body) := by
  cases fuel with
  | zero => simp only [execRangeLoop]; exact MOK_trap _
  | succ fuel =>
    simp only [execRangeLoop]
    apply MOK_bind MOK_tickLoop
    intro u
    split
    · exact MOK_pure _
    · cases iCell with
      | some icell =>
        apply MOK_bind (MOK_writeCell _ _)
        intro u2
        cases vCell with
        | some vcell =>
          apply MOK_bind (helem k)
          intro __dl
          apply MOK_bind (MOK_writeCell _ _)
          intro u3
          apply MOK_bind (MOK_execStmts fuel c fr body)
          intro fres
          obtain ⟨f, fr2⟩ := fres
          simp only []
          cases f with
          | brk => exact MOK_pure _
          | retv vs => exact MOK_pure _
          | normal =>
            exact MOK_execRangeLoop fuel c fr (k + 1) n _ _ elemAt body helem
          | cont =>
            exact MOK_execRangeLoop fuel c fr (k + 1) n _ _ elemAt body helem
        | none =>
          apply MOK_bind (MOK_execStmts fuel c fr body)
          intro fres
          obtain ⟨f, fr2⟩ := fres
          simp only []
          cases f with
          | brk => exact MOK_pure _
          | retv vs => exact MOK_pure _
          | normal =>
            exact MOK_execRangeLoop fuel c fr (k + 1) n _ _ elemAt body helem
          | cont =>
            exact MOK_execRangeLoop fuel c fr (k + 1) n _ _ elemAt body helem
      | none =>
        cases vCell with
        | some vcell =>
          apply MOK_bind (helem k)
          intro __dl
          apply MOK_bind (MOK_writeCell _ _)
          intro u3
          apply MOK_bind (MOK_execStmts fuel c fr body)
          intro fres
          obtain ⟨f, fr2⟩ := fres
          simp only []
          cases f with
          | brk => exact MOK_pure _
          | retv vs => exact MOK_pure _
          | normal =>
            exact MOK_execRangeLoop fuel c fr (k + 1) n _ _ elemAt body helem
          | cont =>
            exact MOK_execRangeLoop fuel c fr (k + 1) n _ _ elemAt body helem
        | none =>
          apply MOK_bind (MOK_execStmts fuel c fr body)
          intro fres
          obtain ⟨f, fr2⟩ := fres
          simp only []
          cases f with
          | brk => exact MOK_pure _
          | retv vs => exact MOK_pure _
          | normal =>
            exact MOK_execRangeLoop fuel c fr (k + 1) n _ _ elemAt body helem
          | cont =>
            exact MOK_execRangeLoop fuel c fr (k + 1) n _ _ elemAt body helem

theorem MOK_execStmt (fuel : Nat) (c : Ctx) (fr : Frame)
    (st : TStmt) : MOK (execStmt fuel c fr st) := by
  cases st with
  | newVar slot init =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalExpr fuel c fr init)
      intro v
      apply MOK_bind (MOK_rebindSlot fr slot v)
      intro a
      obtain ⟨fr', cell⟩ := a
      simp only []
      exact MOK_pure _
  | defineCall2 s1 s2 call =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalCallExpr fuel c fr call)
      intro vs
      split
      · cases s1 with
        | some slot1 =>
          apply MOK_bind (MOK_rebindSlot _ _ _)
          intro x1
          cases s2 with
          | some slot2 =>
            apply MOK_bind (MOK_rebindSlot _ _ _)
            intro x2
            exact MOK_pure _
          | none => exact MOK_pure _
        | none =>
          cases s2 with
          | some slot2 =>
            apply MOK_bind (MOK_rebindSlot _ _ _)
            intro x2
            exact MOK_pure _
          | none => exact MOK_pure _
      · exact MOK_trap _
  | assign1 lhs value =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      cases lhs with
      | some p =>
        apply MOK_bind (MOK_evalPlace fuel c fr p)
        intro a
        obtain ⟨obj, g, path⟩ := a
        simp only []
        apply MOK_bind (MOK_evalExpr fuel c fr value)
        intro v
        apply MOK_bind (MOK_writeLoc _ _ _ _)
        intro u
        exact MOK_pure _
      | none =>
        apply MOK_bind (MOK_evalExpr fuel c fr value)
        intro v
        exact MOK_pure _
  | assignCall2 l1 l2 call =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      cases l1 with
      | some p1 =>
        apply MOK_bind (MOK_evalPlace fuel c fr p1)
        intro a1
        apply MOK_bind (MOK_pure _)
        intro loc1
        cases l2 with
        | some p2 =>
          apply MOK_bind (MOK_evalPlace fuel c fr p2)
          intro a2
          apply MOK_bind (MOK_pure _)
          intro loc2
          apply MOK_bind (MOK_evalCallExpr fuel c fr call)
          intro vs
          split
          · cases loc1 with
            | some l1v =>
              obtain ⟨obj1, g1, p1⟩ := l1v
              simp only []
              apply MOK_bind (MOK_writeLoc _ _ _ _)
              intro u1
              cases loc2 with
              | some l2v =>
                obtain ⟨obj2, g2, p2⟩ := l2v
                simp only []
                apply MOK_bind (MOK_writeLoc _ _ _ _)
                intro u2
                exact MOK_pure _
              | none => exact MOK_pure _
            | none =>
              cases loc2 with
              | some l2v =>
                obtain ⟨obj2, g2, p2⟩ := l2v
                simp only []
                apply MOK_bind (MOK_writeLoc _ _ _ _)
                intro u2
                exact MOK_pure _
              | none => exact MOK_pure _
          · exact MOK_trap _
        | none =>
          apply MOK_bind (MOK_pure _)
          intro loc2
          apply MOK_bind (MOK_evalCallExpr fuel c fr call)
          intro vs
          split
          · cases loc1 with
            | some l1v =>
              obtain ⟨obj1, g1, p1⟩ := l1v
              simp only []
              apply MOK_bind (MOK_writeLoc _ _ _ _)
              intro u1
              cases loc2 with
              | some l2v =>
                obtain ⟨obj2, g2, p2⟩ := l2v
                simp only []
                apply MOK_bind (MOK_writeLoc _ _ _ _)
                intro u2
                exact MOK_pure _
              | none => exact MOK_pure _
            | none =>
              cases loc2 with
              | some l2v =>
                obtain ⟨obj2, g2, p2⟩ := l2v
                simp only []
                apply MOK_bind (MOK_writeLoc _ _ _ _)
                intro u2
                exact MOK_pure _
              | none => exact MOK_pure _
          · exact MOK_trap _
      | none =>
        apply MOK_bind (MOK_pure _)
        intro loc1
        cases l2 with
        | some p2 =>
          apply MOK_bind (MOK_evalPlace fuel c fr p2)
          intro a2
          apply MOK_bind (MOK_pure _)
          intro loc2
          apply MOK_bind (MOK_evalCallExpr fuel c fr call)
          intro vs
          split
          · cases loc1 with
            | some l1v =>
              obtain ⟨obj1, g1, p1⟩ := l1v
              simp only []
              apply MOK_bind (MOK_writeLoc _ _ _ _)
              intro u1
              cases loc2 with
              | some l2v =>
                obtain ⟨obj2, g2, p2⟩ := l2v
                simp only []
                apply MOK_bind (MOK_writeLoc _ _ _ _)
                intro u2
                exact MOK_pure _
              | none => exact MOK_pure _
            | none =>
              cases loc2 with
              | some l2v =>
                obtain ⟨obj2, g2, p2⟩ := l2v
                simp only []
                apply MOK_bind (MOK_writeLoc _ _ _ _)
                intro u2
                exact MOK_pure _
              | none => exact MOK_pure _
          · exact MOK_trap _
        | none =>
          apply MOK_bind (MOK_pure _)
          intro loc2
          apply MOK_bind (MOK_evalCallExpr fuel c fr call)
          intro vs
          split
          · cases loc1 with
            | some l1v =>
              obtain ⟨obj1, g1, p1⟩ := l1v
              simp only []
              apply MOK_bind (MOK_writeLoc _ _ _ _)
              intro u1
              cases loc2 with
              | some l2v =>
                obtain ⟨obj2, g2, p2⟩ := l2v
                simp only []
                apply MOK_bind (MOK_writeLoc _ _ _ _)
                intro u2
                exact MOK_pure _
              | none => exact MOK_pure _
            | none =>
              cases loc2 with
              | some l2v =>
                obtain ⟨obj2, g2, p2⟩ := l2v
                simp only []
                apply MOK_bind (MOK_writeLoc _ _ _ _)
                intro u2
                exact MOK_pure _
              | none => exact MOK_pure _
          · exact MOK_trap _
  | opAssignA op w lhs value =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalPlace fuel c fr lhs)
      intro a
      obtain ⟨obj, g, path⟩ := a
      simp only []
      apply MOK_bind (MOK_evalExpr fuel c fr value)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro b
      apply MOK_bind (MOK_readLoc _ _ _)
      intro __dl2
      apply MOK_bind (MOK_expectInt __dl2)
      intro a2
      split
      · apply MOK_bind (MOK_writeLoc _ _ _ _)
        intro u
        exact MOK_pure _
      · exact MOK_trap _
  | opAssignSh left w lhs count =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalPlace fuel c fr lhs)
      intro a
      obtain ⟨obj, g, path⟩ := a
      simp only []
      apply MOK_bind (MOK_evalExpr fuel c fr count)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro n
      apply MOK_bind (MOK_readLoc _ _ _)
      intro __dl2
      apply MOK_bind (MOK_expectInt __dl2)
      intro a2
      split
      · apply MOK_bind (MOK_writeLoc _ _ _ _)
        intro u
        exact MOK_pure _
      · exact MOK_trap _
  | incdec inc w lhs =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalPlace fuel c fr lhs)
      intro a
      obtain ⟨obj, g, path⟩ := a
      simp only []
      apply MOK_bind (MOK_readLoc _ _ _)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro a2
      apply MOK_bind (MOK_writeLoc _ _ _ _)
      intro u
      exact MOK_pure _
  | ifS cond thn els =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalExpr fuel c fr cond)
      intro __dl
      apply MOK_bind (MOK_expectBool __dl)
      intro b
      apply MOK_bind (MOK_execStmts fuel c fr _)
      intro fres
      obtain ⟨f, fr2⟩ := fres
      simp only []
      exact MOK_pure _
  | forS ini cond post body =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      cases ini with
      | some s =>
        apply MOK_bind (MOK_execStmt fuel c fr s)
        intro x
        apply MOK_bind (MOK_pure _)
        intro fr1
        apply MOK_bind (MOK_execForLoop fuel c fr1 cond post body)
        intro f
        exact MOK_pure _
      | none =>
        apply MOK_bind (MOK_pure _)
        intro fr1
        apply MOK_bind (MOK_execForLoop fuel c fr1 cond post body)
        intro f
        exact MOK_pure _
  | rangeSlice iSlot vSlot over body =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalExpr fuel c fr over)
      intro __dl
      apply MOK_bind (MOK_expectSlice __dl)
      intro a
      obtain ⟨base, off, len, cap⟩ := a
      simp only []
      apply MOK_bind (MOK_allocLoopCell fr iSlot)
      intro a1
      obtain ⟨fr1, iC⟩ := a1
      simp only []
      apply MOK_bind (MOK_allocLoopCell fr1 vSlot)
      intro a2
      obtain ⟨fr2, vC⟩ := a2
      simp only []
      apply MOK_bind (MOK_execRangeLoop fuel c fr2 0 len iC vC _ body
        (fun j => MOK_readElem base off j))
      intro f
      exact MOK_pure _
  | rangeArr iSlot vSlot over body =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalExpr fuel c fr over)
      intro ov
      split
      · apply MOK_bind (MOK_allocLoopCell fr iSlot)
        intro a1
        obtain ⟨fr1, iC⟩ := a1
        simp only []
        apply MOK_bind (MOK_allocLoopCell fr1 vSlot)
        intro a2
        obtain ⟨fr2, vC⟩ := a2
        simp only []
        apply MOK_bind (MOK_execRangeLoop fuel c fr2 0 _ iC vC _ body
          (fun j => MOK_pure _))
        intro f
        exact MOK_pure _
      · exact MOK_trap _
  | rangeInt iSlot over body =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalExpr fuel c fr over)
      intro __dl
      apply MOK_bind (MOK_expectInt __dl)
      intro n
      apply MOK_bind (MOK_allocLoopCell fr iSlot)
      intro a1
      obtain ⟨fr1, iC⟩ := a1
      simp only []
      apply MOK_bind (MOK_execRangeLoop fuel c fr1 0 _ iC none _ body
        (fun j => MOK_trap _))
      intro f
      exact MOK_pure _
  | switchS tag cases_ dflt =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalExpr fuel c fr tag)
      intro tv
      apply MOK_bind (MOK_findCase fuel c fr tv cases_ dflt)
      intro body
      apply MOK_bind (MOK_execStmts fuel c fr body)
      intro fres
      obtain ⟨f, fr2⟩ := fres
      simp only []
      exact MOK_pure _
  | breakS => simp only [execStmt]; exact MOK_pure _
  | continueS => simp only [execStmt]; exact MOK_pure _
  | ret values =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalExprs fuel c fr values)
      intro vs
      exact MOK_pure _
  | retCall call =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_evalCallExpr fuel c fr call)
      intro vs
      exact MOK_pure _
  | exprStmt e =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      split
      · apply MOK_bind (MOK_evalCall fuel c fr _ _)
        intro vs
        exact MOK_pure _
      · apply MOK_bind (MOK_evalExpr fuel c fr _)
        intro v
        exact MOK_pure _
  | blockS body =>
    cases fuel with
    | zero => simp only [execStmt]; exact MOK_trap _
    | succ fuel =>
      simp only [execStmt]
      apply MOK_bind (MOK_execStmts fuel c fr body)
      intro fres
      obtain ⟨f, fr2⟩ := fres
      simp only []
      exact MOK_pure _

theorem MOK_anyCaseMatch (fuel : Nat) (c : Ctx) (fr : Frame)
    (tv : Val) (vals : List TExpr) :
    MOK (anyCaseMatch fuel c fr tv vals) := by
  cases fuel with
  | zero => simp only [anyCaseMatch]; exact MOK_trap _
  | succ fuel =>
    simp only [anyCaseMatch]
    cases vals with
    | nil => exact MOK_pure _
    | cons ve rest =>
      apply MOK_bind (MOK_evalExpr fuel c fr ve)
      intro v
      split
      · exact MOK_pure _
      · exact MOK_anyCaseMatch fuel c fr tv rest
      · exact MOK_trap _

theorem MOK_findCase (fuel : Nat) (c : Ctx) (fr : Frame) (tv : Val)
    (cases_ : List (List TExpr × List TStmt)) (dflt : List TStmt) :
    MOK (findCase fuel c fr tv cases_ dflt) := by
  cases fuel with
  | zero => simp only [findCase]; exact MOK_trap _
  | succ fuel =>
    simp only [findCase]
    cases cases_ with
    | nil => exact MOK_pure _
    | cons cse rest =>
      obtain ⟨vals, body⟩ := cse
      simp only []
      apply MOK_bind (MOK_anyCaseMatch fuel c fr tv vals)
      intro b
      split
      · exact MOK_pure _
      · exact MOK_findCase fuel c fr tv rest dflt

theorem MOK_evalCallExpr (fuel : Nat) (c : Ctx) (fr : Frame)
    (ex : TExpr) : MOK (evalCallExpr fuel c fr ex) := by
  cases fuel with
  | zero => simp only [evalCallExpr]; exact MOK_trap _
  | succ fuel =>
    simp only [evalCallExpr]
    split
    · exact MOK_evalCall fuel c fr _ _
    · exact MOK_trap _

end

/-- The harness form: any successful engine call through the
machine keeps every meter counter monotone and returns with the
call depth balanced. Resetting the meter before a call and reading
it after therefore measures exactly that call, which is what the
driver's per-Exec contract check does. -/
theorem callIdx_meterOK (m : Machine) (idx : Nat) (args : List Val)
    (fuel : Nat) (vs : List Val) (m' : Machine)
    (e : m.callIdx idx args fuel = .ok (vs, m')) :
    MeterOK m.heap m'.heap := by
  unfold Machine.callIdx at e
  split at e
  · cases e
  · split at e
    next vs0 h hrun =>
      cases e
      exact MOK_runFn fuel m.ctx _ args m.heap _ h hrun
    next => cases e

end Vego
