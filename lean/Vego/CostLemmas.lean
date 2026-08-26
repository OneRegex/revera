/-
Universal theorems about the cost model.

The corpus theorem checks recorded executions; the statements here
quantify over all inputs and are proved by ordinary induction, so
they hold universally and use no native evaluation. They pin down
the arithmetic behind the resource meter:

- the growth rule of the portable append contract makes any run of
  growing appends allocate at most a fixed multiple of the largest
  buffer it ever needs (the geometric series argument, made
  precise on Nat);
- the layout function that prices buffer elements produces sizes
  divisible by their alignment, with positive alignment, as the
  64-bit layout of every target requires;
- the meter primitives account allocations exactly: a charge adds
  exactly its size, and the heap operations around it never touch
  the counter, so a growing append raises the meter by exactly
  newcap times the element size and an in-place append leaves it
  unchanged.
-/

import Vego.Interp

namespace Vego

/-! ## The growth rule -/

/-- One growth step of the portable append contract, as the
runtimes and `doAppend` implement it. -/
def growCap (cap need : Nat) : Nat :=
  Nat.max (Nat.max (2 * cap) 8) need

theorem growCap_ge_need (cap need : Nat) : need ≤ growCap cap need :=
  Nat.le_max_right _ _

theorem growCap_doubles (cap need : Nat) :
    2 * cap ≤ growCap cap need :=
  Nat.le_trans (Nat.le_max_left _ _) (Nat.le_max_left _ _)

/-- A growth step only runs when the buffer is too small, and then
the new capacity stays within twice the need, plus the floor. -/
theorem growCap_le (cap need n : Nat) (hc : cap < need) (hn : need ≤ n) :
    growCap cap need ≤ 2 * n + 8 :=
  Nat.max_le.mpr ⟨Nat.max_le.mpr ⟨by omega, by omega⟩, by omega⟩

/-- The capacities a buffer goes through, oldest first: every step
at least doubles, which is what `growCap_doubles` gives each grown
capacity. -/
def GrowthChain : List Nat → Prop
  | [] => True
  | [_] => True
  | a :: b :: rest => 2 * a ≤ b ∧ GrowthChain (b :: rest)

def total : List Nat → Nat
  | [] => 0
  | x :: xs => x + total xs

def lastD : List Nat → Nat → Nat
  | [], d => d
  | x :: xs, _ => lastD xs x

/-- The geometric series bound: a doubling chain allocates less
than twice its final capacity, over its whole history. -/
theorem growthChain_total (a : Nat) (l : List Nat)
    (h : GrowthChain (a :: l)) :
    total (a :: l) + a ≤ 2 * lastD (a :: l) 0 := by
  induction l generalizing a with
  | nil => simp [total, lastD]; omega
  | cons b rest ih =>
    obtain ⟨hab, hrest⟩ := h
    have hb := ih b hrest
    simp only [total, lastD] at *
    omega

/-- Every run of growing appends whose needs stay within n
allocates at most 2 * (2n + 8) elements in total, across all the
buffers it ever abandons to the arena. This is the universal form
of the doubling constants the resource contract folds into its
per-record sizes. -/
theorem growthChain_total_le (a : Nat) (l : List Nat) (n : Nat)
    (h : GrowthChain (a :: l)) (hlast : lastD (a :: l) 0 ≤ 2 * n + 8) :
    total (a :: l) ≤ 2 * (2 * n + 8) := by
  have := growthChain_total a l h
  omega

/-- Chains that arise from real append histories: each grown
capacity is one growCap step over the previous capacity, for some
need. `growCap_doubles` turns any such history into a doubling
chain, so the geometric bound applies to every buffer the
interpreter can ever grow, not only to abstract chains. -/
theorem growthChain_of_steps (a : Nat) (l : List Nat)
    (h : ∀ i, (hi : i + 1 < (a :: l).length) →
      ∃ need, (a :: l)[i + 1] = growCap ((a :: l)[i]'(by omega)) need) :
    GrowthChain (a :: l) := by
  induction l generalizing a with
  | nil => trivial
  | cons b rest ih =>
    constructor
    · obtain ⟨need, hb⟩ := h 0 (by simp)
      simp at hb
      rw [hb]
      exact growCap_doubles a need
    · exact ih b (fun i hi => h (i + 1) (by simpa using hi))

/-! ## The layout function -/

/-- Alignments are positive, so the rounding in the layout never
divides by zero. -/
theorem sizeAlignD_align_pos (d : Nat) (structs : Array (Array VTy))
    (ty : VTy) : 1 ≤ (sizeAlignD d structs ty).2 := by
  induction ty generalizing d with
  | bool => simp [sizeAlignD]
  | int w => cases w <;> simp [sizeAlignD, IW.byteSize]
  | str => simp [sizeAlignD]
  | slice _ => simp [sizeAlignD]
  | ptr _ => simp [sizeAlignD]
  | arr n e ih =>
    cases d with
    | zero => simp [sizeAlignD]
    | succ d => simpa [sizeAlignD] using ih d
  | strukt si =>
    cases d with
    | zero => simp [sizeAlignD]
    | succ d =>
      simp only [sizeAlignD]
      cases structs[si]? with
      | none => simp
      | some ftys =>
        suffices h : ∀ (l : List VTy) (acc : Nat × Nat), 1 ≤ acc.2 →
            1 ≤ (l.foldl (fun acc fty =>
              let sa := sizeAlignD d structs fty
              (((acc.1 + sa.2 - 1) / sa.2) * sa.2 + sa.1,
               Nat.max acc.2 sa.2)) acc).2 by
          simpa using h ftys.toList (0, 1) (by simp)
        intro l
        induction l with
        | nil => intro acc hacc; simpa using hacc
        | cons f rest ihl =>
          intro acc hacc
          apply ihl
          exact Nat.le_trans hacc (Nat.le_max_left _ _)

/-- The layout sizes are multiples of their alignment, exactly as
the 64-bit layout of the targets requires: array strides need no
extra padding, and struct sizes end at their own alignment. -/
theorem sizeAlignD_align_dvd (d : Nat) (structs : Array (Array VTy))
    (ty : VTy) :
    (sizeAlignD d structs ty).2 ∣ (sizeAlignD d structs ty).1 := by
  induction ty generalizing d with
  | bool => simp [sizeAlignD]
  | int w => cases w <;> simp [sizeAlignD, IW.byteSize]
  | str => simp [sizeAlignD]
  | slice _ => simp [sizeAlignD]
  | ptr _ => simp [sizeAlignD]
  | arr n e ih =>
    cases d with
    | zero => simp [sizeAlignD]
    | succ d =>
      simp only [sizeAlignD]
      obtain ⟨k, hk⟩ := ih d
      exact ⟨n * k, by rw [hk, Nat.mul_left_comm]⟩
  | strukt si =>
    cases d with
    | zero => simp [sizeAlignD]
    | succ d =>
      simp only [sizeAlignD]
      cases structs[si]? with
      | none => simp
      | some ftys =>
        simp only []
        exact Nat.dvd_mul_left _ _

/-! ## The meter primitives -/

/-- The monadic glue, definitionally: running a bind runs the
first action and feeds its heap to the second. -/
theorem M.bind_def {α β : Type} (x : M α) (f : α → M β) (h : Heap) :
    (x >>= f) h = match x h with
      | .ok v h' => f v h'
      | .trap t => .trap t := rfl

theorem M.pure_def {α : Type} (a : α) (h : Heap) :
    (pure a : M α) h = .ok a h := rfl

/-- A charge adds exactly its size to the allocation meter. -/
theorem charge_allocBytes (b : Nat) (h h' : Heap) (u : Unit)
    (e : M.charge b h = .ok u h') :
    h'.allocBytes = h.allocBytes + b := by
  unfold M.charge at e
  cases e
  rfl

theorem alloc_allocBytes (v : Val) (h h' : Heap) (r : Nat × Nat)
    (e : M.alloc v h = .ok r h') :
    h'.allocBytes = h.allocBytes := by
  unfold M.alloc at e
  split at e
  · split at e
    · cases e; rfl
    · cases e
  · cases e; rfl

theorem readLoc_heap (obj gen : Nat) (path : Path) (h h' : Heap)
    (v : Val) (e : M.readLoc obj gen path h = .ok v h') : h' = h := by
  unfold M.readLoc at e
  split at e
  · split at e
    · cases e
    · split at e
      · cases e; rfl
      · cases e
  · cases e

theorem writeLoc_allocBytes (obj gen : Nat) (path : Path) (nv : Val)
    (h h' : Heap) (u : Unit)
    (e : M.writeLoc obj gen path nv h = .ok u h') :
    h'.allocBytes = h.allocBytes := by
  unfold M.writeLoc at e
  split at e
  · cases e
  · split at e
    · cases e
    · split at e <;> first
        | (cases e; rfl)
        | (split at e <;> try split at e) <;> (try cases e) <;> rfl

theorem readElems_heap (base : Option Loc) (off len : Nat)
    (h h' : Heap) (vs : Array Val)
    (e : M.readElems base off len h = .ok vs h') : h' = h := by
  unfold M.readElems at e
  split at e
  · cases e; rfl
  · split at e
    · cases e
    · simp only [M.bind_def] at e
      split at e
      next x h2 hloc =>
        split at e
        · split at e
          · cases e
          · cases e
            exact readLoc_heap _ _ _ _ _ _ hloc
        · cases e
      next => cases e

theorem writeElems_allocBytes (base : Option Loc) (off k : Nat)
    (vs : Array Val) (h h' : Heap) (u : Unit)
    (e : M.writeElems base off k vs h = .ok u h') :
    h'.allocBytes = h.allocBytes := by
  unfold M.writeElems at e
  split at e
  · cases e; rfl
  · split at e
    · cases e
    · simp only [M.bind_def] at e
      split at e
      next x h2 hloc =>
        split at e
        · split at e
          · cases e
          · have h2h := readLoc_heap _ _ _ _ _ _ hloc
            subst h2h
            exact writeLoc_allocBytes _ _ _ _ _ _ _ e
        · cases e
      next => cases e

/-- A make charges exactly its capacity priced at the element
size. -/
theorem doMake_allocBytes (c : Ctx) (elemTy : VTy) (n cp : Int)
    (h h' : Heap) (v : Val) (e : doMake c elemTy n cp h = .ok v h') :
    h'.allocBytes = h.allocBytes +
      cp.toNat * elemBytes c.structs elemTy := by
  unfold doMake at e
  split at e
  · cases e
  · simp only [M.bind_def] at e
    split at e
    next u h1 hch =>
      split at e
      next cg h2 hal =>
        cases e
        rw [alloc_allocBytes _ _ _ _ hal, charge_allocBytes _ _ _ _ hch]
      next => cases e
    next => cases e

/-- A slice literal charges exactly its element count priced at
the element size. -/
theorem doSliceLit_allocBytes (c : Ctx) (elemTy : VTy)
    (vs : Array Val) (h h' : Heap) (v : Val)
    (e : doSliceLit c elemTy vs h = .ok v h') :
    h'.allocBytes = h.allocBytes +
      vs.size * elemBytes c.structs elemTy := by
  unfold doSliceLit at e
  simp only [M.bind_def] at e
  split at e
  next u h1 hch =>
    split at e
    next cg h2 hal =>
      cases e
      rw [alloc_allocBytes _ _ _ _ hal, charge_allocBytes _ _ _ _ hch]
    next => cases e
  next => cases e

/-- A string-to-bytes conversion charges exactly one byte per
character of the string. -/
theorem doStrToBytes_allocBytes (s : ByteArray) (h h' : Heap)
    (v : Val) (e : doStrToBytes s h = .ok v h') :
    h'.allocBytes = h.allocBytes + s.size := by
  unfold doStrToBytes at e
  simp only [M.bind_def] at e
  split at e
  next u h1 hch =>
    split at e
    next cg h2 hal =>
      cases e
      rw [alloc_allocBytes _ _ _ _ hal, charge_allocBytes _ _ _ _ hch]
    next => cases e
  next => cases e

/-- A bytes-to-string conversion charges exactly one byte per
element. -/
theorem doBytesToStr_allocBytes (vs : Array Val) (h h' : Heap)
    (v : Val) (e : doBytesToStr vs h = .ok v h') :
    h'.allocBytes = h.allocBytes + vs.size := by
  unfold doBytesToStr at e
  simp only [M.bind_def] at e
  split at e
  next u h1 hch =>
    cases hv : valsToBytes vs with
    | some out =>
      rw [hv] at e
      cases e
      exact charge_allocBytes _ _ _ _ hch
    | none => rw [hv] at e; cases e
  next => cases e

/-- The in-place branch of an append never touches the allocation
meter, and the growing branch raises it by exactly the new
capacity priced at the element size: the meter counts what the
arena of a target would hand out, nothing else. -/
theorem doAppend_allocBytes (c : Ctx) (base : Option Loc)
    (off len cap : Nat) (adds : Array Val) (elemTy : VTy)
    (h h' : Heap) (v : Val)
    (e : doAppend c (.slice base off len cap) adds elemTy h = .ok v h') :
    (adds.size = 0 ∨ len + adds.size ≤ cap →
      h'.allocBytes = h.allocBytes) ∧
    (adds.size ≠ 0 → cap < len + adds.size →
      h'.allocBytes = h.allocBytes +
        growCap cap (len + adds.size) * elemBytes c.structs elemTy) := by
  unfold doAppend at e
  simp only [M.expectSlice, M.bind_def, M.pure_def] at e
  split at e
  case _ hz =>
    cases e
    simp_all
  case _ hz =>
    split at e
    case _ hle =>
      constructor
      · intro _
        simp only [M.bind_def] at e
        split at e
        next u h1 hwe =>
          cases e
          exact writeElems_allocBytes _ _ _ _ _ _ _ hwe
        next => cases e
      · intro _ hcap
        omega
    case _ hle =>
      constructor
      · intro hor
        rcases hor with h0 | hcap
        · simp [h0] at hz
        · omega
      · intro _ _
        simp only [M.bind_def] at e
        split at e
        next u h1 hch =>
          split at e
          next live h2 hre =>
            split at e
            next cg h3 hal =>
              cases e
              have h21 := readElems_heap _ _ _ _ _ _ hre
              subst h21
              have hc := charge_allocBytes _ _ _ _ hch
              have ha := alloc_allocBytes _ _ _ _ hal
              rw [ha, hc]
              rfl
            next => cases e
          next => cases e
        next => cases e

/-- Writing elements into place never changes the buffer size. -/
theorem blitInto_size (es : Array Val) (base : Nat) (vs : Array Val) :
    (blitInto es base vs).size = es.size := by
  unfold blitInto
  generalize vs.size = m
  induction m with
  | zero => rfl
  | succ m ih =>
    simp only [Array.set!] at ih ⊢
    simp [Nat.fold_succ, Array.size_setIfInBounds, ih]

/-! ## The saturating contract arithmetic

contract.go computes every figure with cAdd and cMul, which
saturate at 1<<62 to mark a bound too large to be useful. The
contract only ever feeds them values in [0, 1<<62], so the Nat
mirror below is exact on that domain, and the theorems pin the
algebra down: figures never pass the saturation mark, and a figure
below the mark is the exact arithmetic value, so saturation can
flag but never shrink a bound. -/

def contractCap : Nat := 4611686018427387904

def cAdd (a b : Nat) : Nat :=
  if contractCap - b < a then contractCap else a + b

def cMul (a b : Nat) : Nat :=
  if a = 0 ∨ b = 0 then 0
  else if contractCap / b < a then contractCap else a * b

theorem cAdd_le_cap (a b : Nat) (hb : b ≤ contractCap) :
    cAdd a b ≤ contractCap := by
  unfold cAdd
  split <;> omega

theorem cAdd_exact (a b : Nat) (h : cAdd a b < contractCap) :
    cAdd a b = a + b := by
  unfold cAdd at h ⊢
  split
  · next hs => rw [if_pos hs] at h; omega
  · rfl

theorem cMul_le_cap (a b : Nat) : cMul a b ≤ contractCap := by
  unfold cMul
  split
  · exact Nat.zero_le _
  · next hz =>
    split
    · exact Nat.le_refl _
    · next hle =>
      calc a * b ≤ contractCap / b * b :=
            Nat.mul_le_mul_right b (by omega)
        _ ≤ contractCap := Nat.div_mul_le_self _ _

theorem cMul_exact (a b : Nat) (h : cMul a b < contractCap) :
    cMul a b = a * b := by
  unfold cMul at h ⊢
  split
  · next hz => rcases hz with h0 | h0 <;> simp [h0]
  · next hz =>
    split
    · next hsat =>
      rw [if_neg hz, if_pos hsat] at h
      omega
    · rfl

end Vego
