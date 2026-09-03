/-
Functional correctness of the phase A model: it computes the selected match.

The reference is a path semantics over the compiled program. A thread is an instruction with a start and a
counter vector at a subject position; it moves along epsilon edges at its position and along consuming
edges to later positions, and it begins as a spawn at every character boundary. A candidate is a thread at
the accept instruction. The selected match is the candidate with the earliest start, then the smallest
counters in the lexicographic order the engine uses, then the longest end.

This file states the reference and proves the closure of one boundary against it; `PhaseARun.lean` takes the
consuming phase, the jumps and the loop through to `run_correct`, which says the model reports a match exactly
when a candidate exists, and that what it reports is the selected candidate. The proof keeps, boundary by
boundary, that every stored payload is a thread of the reference, and that every productive thread of the
reference is either held by a payload at least as good or dominated by the best candidate found so far.
Merging keeps the best payload per instruction because two threads at the same instruction and position have
identical futures, and the lexicographic order survives adding the same counter increments to both.
-/

import Vego.PhaseAProofs

namespace Vego
namespace PhaseA

/-! ## Positions, characters and anchors of the subject -/

/-- The character at a boundary: the end marker past the subject, else the decoded character. -/
def curAt (input : Input) (pos : Nat) : Int :=
  if pos == input.bytes.size then -2 else (decodeRuneAt input.bytes pos).1

/-- The byte size of the character at a boundary, zero at the end. -/
def sizeAt (input : Input) (pos : Nat) : Nat :=
  if pos == input.bytes.size then 0 else (decodeRuneAt input.bytes pos).2

/-- The next boundary. -/
def stepPos (input : Input) (pos : Nat) : Nat := pos + sizeAt input pos

/-- The boundary `d` characters ahead. -/
def advance (input : Input) (pos : Nat) : Nat → Nat
  | 0 => pos
  | d + 1 => advance input (stepPos input pos) d

/-- The byte position of the boundary with index `i`: `i` characters from the start. -/
def chainPos (input : Input) (i : Nat) : Nat := advance input 0 i

/-- A character boundary: a position the decoding chain from the start reaches. -/
def Chain (input : Input) (pos : Nat) : Prop := ∃ n, chainPos input n = pos

/-- The lookahead of up to `fuel` characters from a position, as `decodeAhead` decodes it. -/
def aheadList (input : Input) (pos : Nat) : Nat → List Int
  | 0 => []
  | fuel + 1 =>
    if pos < input.bytes.size then (decodeRuneAt input.bytes pos).1 :: aheadList input (stepPos input pos) fuel
    else []

def aheadAt (input : Input) (pos : Nat) : List Int := aheadList input pos maxElemAhead

/-- `^` holds at the boundary with index `i`: the subject start unless `REG_NOTBOL`, or right after a newline
character in newline mode. -/
def bolRef (input : Input) (i : Nat) : Prop :=
  (i = 0 ∧ input.notbol = false) ∨ (input.nlMode = true ∧ 0 < i ∧ curAt input (chainPos input (i - 1)) = 10)

/-- `$` holds at the boundary with index `i`: the subject end unless `REG_NOTEOL`, or right before a newline. -/
def eolRef (input : Input) (i : Nat) : Prop :=
  (chainPos input i = input.bytes.size ∧ input.noteol = false) ∨ (input.nlMode = true ∧ curAt input (chainPos input i) = 10)

/-! ## Threads and their steps -/

/-- A thread: at a boundary (by index), at an instruction, with the start it carries and its counters. -/
structure Th where
  i : Nat
  pc : Nat
  s : Nat
  c : List Nat
  deriving Repr, DecidableEq

/-- The byte position of a thread's boundary. -/
def Th.pos (input : Input) (T : Th) : Nat := chainPos input T.i

def Prog.op (p : Prog) (pc : Nat) : Op := (p.ins.getD pc default).op
def Prog.next (p : Prog) (pc : Nat) : Nat := (p.ins.getD pc default).next
def Prog.alt (p : Prog) (pc : Nat) : Nat := (p.ins.getD pc default).alt
def Prog.slotsOf (p : Prog) (pc : Nat) : List Nat := (p.ins.getD pc default).slots

/-- The counters after a consuming step, as `paArrive` computes them. -/
def bumpIf (p : Prog) (pc : Nat) (c : List Nat) (delta : Nat) : List Nat :=
  if p.k > 0 && !(p.slotsOf pc).isEmpty then bumpCtr c (p.slotsOf pc) delta else c

/-- An epsilon edge of the program at the boundary with index `i`. -/
def EpsEdge (p : Prog) (input : Input) (i pc q : Nat) : Prop :=
  match p.op pc with
  | .split => q = p.next pc ∨ q = p.alt pc
  | .jmp => q = p.next pc
  | .bol => bolRef input i ∧ q = p.next pc
  | .eol => eolRef input i ∧ q = p.next pc
  | _ => False

/-- A consuming edge of `delta` characters at the boundary with index `i`: the single test, or one probe. -/
def Consumes (p : Prog) (atoms : Atoms) (input : Input) (i pc delta : Nat) : Prop :=
  (p.op pc).consuming = true ∧
  ((delta = 1 ∧ chainPos input i < input.bytes.size ∧ atoms.single pc (curAt input (chainPos input i)) = true) ∨
   (p.op pc = .bracket ∧ delta ∈ atoms.lens pc ∧ delta ≤ (aheadAt input (chainPos input i)).length ∧
    atoms.multi pc ((aheadAt input (chainPos input i)).take delta) = true))

/-- One step of a thread. -/
inductive Step (p : Prog) (atoms : Atoms) (input : Input) : Th → Th → Prop
  | eps (T : Th) (q : Nat) (h : EpsEdge p input T.i T.pc q) : Step p atoms input T { T with pc := q }
  | consume (T : Th) (delta : Nat) (h : Consumes p atoms input T.i T.pc delta) :
      Step p atoms input T ⟨T.i + delta, p.next T.pc, T.s, bumpIf p T.pc T.c delta⟩

/-- Zero or more steps. -/
inductive Steps (p : Prog) (atoms : Atoms) (input : Input) : Th → Th → Prop
  | refl (T : Th) : Steps p atoms input T T
  | tail (a b c : Th) (h : Steps p atoms input a b) (s : Step p atoms input b c) : Steps p atoms input a c

/-- The thread the boundary with index `i` spawns: it starts at that boundary's position. -/
def spawnTh (p : Prog) (input : Input) (i : Nat) : Th := ⟨i, p.start, chainPos input i, List.replicate p.k 0⟩

/-- A boundary index of the subject: the first, or one whose predecessor sits before the end. -/
def ValidIdx (input : Input) (i : Nat) : Prop := i = 0 ∨ chainPos input (i - 1) < input.bytes.size

/-- A thread of the reference: reached from a spawn at some boundary of the subject. -/
def Reach (p : Prog) (atoms : Atoms) (input : Input) (T : Th) : Prop :=
  ∃ i, ValidIdx input i ∧ Steps p atoms input (spawnTh p input i) T

/-- A thread that can still accept. -/
def Prod (p : Prog) (atoms : Atoms) (input : Input) (T : Th) : Prop :=
  ∃ T', Steps p atoms input T T' ∧ p.op T'.pc = .accept

/-- A candidate: the start, counters and end position of a thread at the accept instruction. -/
def Cand (p : Prog) (atoms : Atoms) (input : Input) (s : Nat) (c : List Nat) (e : Nat) : Prop :=
  ∃ i pc, Reach p atoms input ⟨i, pc, s, c⟩ ∧ p.op pc = .accept ∧ chainPos input i = e

/-- The selection order: earliest start, then smallest counters, then longest end. -/
def selLE (a b : Nat × List Nat × Nat) : Prop :=
  a.1 < b.1 ∨ (a.1 = b.1 ∧ (ctrLess a.2.1 b.2.1 = true ∨ (a.2.1 = b.2.1 ∧ b.2.2 ≤ a.2.2)))

/-- The selected match: a candidate that the order puts at or before every candidate. -/
def IsBest (p : Prog) (atoms : Atoms) (input : Input) (s : Nat) (c : List Nat) (e : Nat) : Prop :=
  Cand p atoms input s c e ∧ ∀ s' c' e', Cand p atoms input s' c' e' → selLE (s, c, e) (s', c', e')

/-! ## The lexicographic order under increments -/

theorem ctrLess_total (a b : List Nat) (h : a.length = b.length) :
    ctrLess a b = true ∨ ctrLess b a = true ∨ a = b := by
  induction a generalizing b with
  | nil => cases b with
    | nil => right; right; rfl
    | cons y ys => simp at h
  | cons x xs ih =>
    cases b with
    | nil => simp at h
    | cons y ys =>
      simp only [List.length_cons, Nat.add_right_cancel_iff] at h
      by_cases hxy : x = y
      · subst hxy
        rcases ih ys h with h1 | h1 | h1
        · left; simp [ctrLess, h1]
        · right; left; simp [ctrLess, h1]
        · right; right; rw [h1]
      · rcases Nat.lt_or_gt_of_ne hxy with hlt | hgt
        · left; simp [ctrLess, hxy, hlt]
        · right; left; simp [ctrLess, Ne.symm hxy, hgt]

theorem ctrLess_asymm (a b : List Nat) (h : ctrLess a b = true) : ctrLess b a = false := by
  cases hba : ctrLess b a
  · rfl
  · have := ctrLess_trans a b a h hba
    rw [ctrLess_irrefl] at this
    exact absurd this (by simp)

theorem ctrLess_length (a b : List Nat) (h : ctrLess a b = true) : a ≠ b := by
  intro heq; subst heq; rw [ctrLess_irrefl] at h; exact absurd h (by simp)

/-- `bumpCtr` from an index: each position gets the increment when its index is a selected slot. -/
def bumpFrom (slots : List Nat) (delta : Nat) : List Nat → Nat → List Nat
  | [], _ => []
  | x :: xs, n => (if slots.contains n then x + delta else x) :: bumpFrom slots delta xs (n + 1)

theorem bumpCtr_eq_bumpFrom (c slots : List Nat) (delta : Nat) : bumpCtr c slots delta = bumpFrom slots delta c 0 := by
  unfold bumpCtr
  suffices h : ∀ n, (c.zipIdx n).map (fun (v, i) => if slots.contains i then v + delta else v) = bumpFrom slots delta c n from h 0
  intro n
  induction c generalizing n with
  | nil => rfl
  | cons x xs ih =>
    simp only [List.zipIdx_cons, List.map_cons, bumpFrom]
    rw [ih]

theorem bumpFrom_length (slots : List Nat) (delta : Nat) (c : List Nat) (n : Nat) :
    (bumpFrom slots delta c n).length = c.length := by
  induction c generalizing n with
  | nil => rfl
  | cons x xs ih => simp [bumpFrom, ih]

theorem bumpCtr_length (c slots : List Nat) (delta : Nat) : (bumpCtr c slots delta).length = c.length := by
  rw [bumpCtr_eq_bumpFrom]; exact bumpFrom_length _ _ _ _

/-- Adding the same increments to both sides keeps the lexicographic order. -/
theorem ctrLess_bumpFrom (slots : List Nat) (delta : Nat) (a b : List Nat) (n : Nat) (h : ctrLess a b = true) :
    ctrLess (bumpFrom slots delta a n) (bumpFrom slots delta b n) = true := by
  induction a generalizing b n with
  | nil => simp [ctrLess] at h
  | cons x xs ih =>
    cases b with
    | nil => simp [ctrLess] at h
    | cons y ys =>
      simp only [ctrLess, bumpFrom, bne_iff_ne, ne_eq, ite_not] at h ⊢
      split at h
      · rename_i hxy; subst hxy; simp; exact ih ys (n + 1) h
      · rename_i hxy
        simp only [decide_eq_true_eq] at h
        split
        · have : ¬ (x + delta = y + delta) := by omega
          rw [if_neg this]; simp; omega
        · simp [hxy]; omega

/-- A strictly smaller vector stays strictly smaller than the other side after increments on that side alone. -/
theorem ctrLess_bumpFrom_right (slots : List Nat) (delta : Nat) (a b : List Nat) (n : Nat) (h : ctrLess a b = true) :
    ctrLess a (bumpFrom slots delta b n) = true := by
  induction a generalizing b n with
  | nil => simp [ctrLess] at h
  | cons x xs ih =>
    cases b with
    | nil => simp [ctrLess] at h
    | cons y ys =>
      simp only [ctrLess, bumpFrom, bne_iff_ne, ne_eq, ite_not] at h ⊢
      split at h
      · rename_i hxy; subst hxy
        split
        · by_cases hd : delta = 0
          · subst hd; simp; exact ih ys (n + 1) h
          · have : ¬ (x = x + delta) := by omega
            rw [if_neg this]; simp; omega
        · simp; exact ih ys (n + 1) h
      · rename_i hxy
        simp only [decide_eq_true_eq] at h
        split
        · have : ¬ (x = y + delta) := by omega
          rw [if_neg this]; simp; omega
        · simp [hxy]; omega

theorem ctrLess_bumpCtr (slots : List Nat) (delta : Nat) (a b : List Nat) (h : ctrLess a b = true) :
    ctrLess (bumpCtr a slots delta) (bumpCtr b slots delta) = true := by
  rw [bumpCtr_eq_bumpFrom, bumpCtr_eq_bumpFrom]; exact ctrLess_bumpFrom _ _ _ _ _ h

theorem ctrLess_bumpCtr_right (slots : List Nat) (delta : Nat) (a b : List Nat) (h : ctrLess a b = true) :
    ctrLess a (bumpCtr b slots delta) = true := by
  rw [bumpCtr_eq_bumpFrom]; exact ctrLess_bumpFrom_right _ _ _ _ _ h

theorem bumpIf_length (p : Prog) (pc : Nat) (c : List Nat) (delta : Nat) : (bumpIf p pc c delta).length = c.length := by
  unfold bumpIf; split
  · exact bumpCtr_length _ _ _
  · rfl

theorem ctrLess_bumpIf (p : Prog) (pc : Nat) (delta : Nat) (a b : List Nat) (h : ctrLess a b = true) :
    ctrLess (bumpIf p pc a delta) (bumpIf p pc b delta) = true := by
  unfold bumpIf; split
  · exact ctrLess_bumpCtr _ _ _ _ h
  · exact h

theorem ctrLess_bumpIf_right (p : Prog) (pc : Nat) (delta : Nat) (a b : List Nat) (h : ctrLess a b = true) :
    ctrLess a (bumpIf p pc b delta) = true := by
  unfold bumpIf; split
  · exact ctrLess_bumpCtr_right _ _ _ _ h
  · exact h

/-- The payload order survives a consuming step taken by both threads. -/
theorem plt_bumpIf (p : Prog) (pc : Nat) (delta : Nat) (a b : Payload) (h : plt p.k a b = true) :
    plt p.k (a.1, bumpIf p pc a.2 delta) (b.1, bumpIf p pc b.2 delta) = true := by
  simp only [plt, Bool.or_eq_true, Bool.and_eq_true, decide_eq_true_eq, beq_iff_eq, bne_iff_ne, ne_eq] at h ⊢
  rcases h with h | ⟨⟨h1, h2⟩, h3⟩
  · left; exact h
  · right; exact ⟨⟨h1, h2⟩, ctrLess_bumpIf p pc delta _ _ h3⟩

/-! ## The payload order as a total order on vectors of one length -/

theorem plt_asymm (k : Nat) (a b : Payload) (h : plt k a b = true) : plt k b a = false := by
  cases hba : plt k b a
  · rfl
  · have := plt_trans k a b a h hba
    rw [plt_irrefl] at this
    exact absurd this (by simp)

/-- Payloads the engine cannot tell apart: the same start, and the same counters when counters count. -/
def peq (k : Nat) (a b : Payload) : Prop := a.1 = b.1 ∧ (k = 0 ∨ a.2 = b.2)

theorem peq_refl (k : Nat) (a : Payload) : peq k a a := ⟨rfl, Or.inr rfl⟩
theorem peq_symm (k : Nat) (a b : Payload) (h : peq k a b) : peq k b a :=
  ⟨h.1.symm, h.2.elim Or.inl (fun e => Or.inr e.symm)⟩
theorem peq_trans (k : Nat) (a b c : Payload) (h1 : peq k a b) (h2 : peq k b c) : peq k a c :=
  ⟨h1.1.trans h2.1, by
    rcases h1.2 with h | h
    · exact Or.inl h
    · rcases h2.2 with h' | h'
      · exact Or.inl h'
      · exact Or.inr (h.trans h')⟩

theorem plt_peq_right (k : Nat) (a b c : Payload) (h : peq k b c) : plt k a b = plt k a c := by
  obtain ⟨h1, h2⟩ := h
  simp only [plt]
  rw [h1]
  rcases h2 with h2 | h2
  · subst h2; simp
  · rw [h2]

theorem plt_peq_left (k : Nat) (a b c : Payload) (h : peq k a b) : plt k a c = plt k b c := by
  obtain ⟨h1, h2⟩ := h
  simp only [plt]
  rw [h1]
  rcases h2 with h2 | h2
  · subst h2; simp
  · rw [h2]

theorem plt_irrefl_peq (k : Nat) (a b : Payload) (h : peq k a b) : plt k a b = false := by
  rw [plt_peq_right k a b a (peq_symm k a b h)]; exact plt_irrefl k a

/-- On payloads whose counters have one length, the order is total up to `peq`. -/
theorem plt_total (k : Nat) (a b : Payload) (hl : a.2.length = b.2.length) :
    plt k a b = true ∨ plt k b a = true ∨ peq k a b := by
  obtain ⟨sa, ca⟩ := a
  obtain ⟨sb, cb⟩ := b
  simp only [plt, peq, Bool.or_eq_true, Bool.and_eq_true, decide_eq_true_eq, beq_iff_eq, bne_iff_ne, ne_eq]
  rcases Nat.lt_trichotomy sa sb with h | h | h
  · left; left; exact h
  · subst h
    by_cases hk : k = 0
    · right; right; exact ⟨rfl, Or.inl hk⟩
    · rcases ctrLess_total ca cb hl with hc | hc | hc
      · left; right; exact ⟨⟨rfl, hk⟩, hc⟩
      · right; left; right; exact ⟨⟨rfl, hk⟩, hc⟩
      · right; right; exact ⟨rfl, Or.inr hc⟩
  · right; left; left; exact h

/-- At or below: strictly below, or indistinguishable. -/
def ple (k : Nat) (a b : Payload) : Prop := plt k a b = true ∨ peq k a b

theorem ple_refl (k : Nat) (a : Payload) : ple k a a := Or.inr (peq_refl k a)

theorem ple_trans (k : Nat) (a b c : Payload) (h1 : ple k a b) (h2 : ple k b c) : ple k a c := by
  rcases h1 with h1 | h1 <;> rcases h2 with h2 | h2
  · exact Or.inl (plt_trans k a b c h1 h2)
  · left; rw [← plt_peq_right k a b c h2]; exact h1
  · left; rw [plt_peq_left k a b c h1]; exact h2
  · exact Or.inr (peq_trans k a b c h1 h2)

theorem ple_of_not_plt (k : Nat) (a b : Payload) (hl : a.2.length = b.2.length) (h : plt k b a = false) :
    ple k a b := by
  rcases plt_total k a b hl with h1 | h1 | h1
  · exact Or.inl h1
  · rw [h1] at h; exact absurd h (by simp)
  · exact Or.inr h1

theorem not_plt_of_ple (k : Nat) (a b : Payload) (h : ple k a b) : plt k b a = false := by
  rcases h with h | h
  · exact plt_asymm k a b h
  · exact plt_irrefl_peq k b a (peq_symm k a b h)

theorem plt_of_plt_ple (k : Nat) (a b c : Payload) (h1 : plt k a b = true) (h2 : ple k b c) : plt k a c = true := by
  rcases h2 with h2 | h2
  · exact plt_trans k a b c h1 h2
  · rw [← plt_peq_right k a b c h2]; exact h1

theorem plt_of_ple_plt (k : Nat) (a b c : Payload) (h1 : ple k a b) (h2 : plt k b c = true) : plt k a c = true := by
  rcases h1 with h1 | h1
  · exact plt_trans k a b c h1 h2
  · rw [plt_peq_left k a b c h1]; exact h2

theorem ple_bumpIf (p : Prog) (pc : Nat) (delta : Nat) (a b : Payload) (h : ple p.k a b) :
    ple p.k (a.1, bumpIf p pc a.2 delta) (b.1, bumpIf p pc b.2 delta) := by
  rcases h with h | h
  · exact Or.inl (plt_bumpIf p pc delta a b h)
  · right
    refine ⟨h.1, ?_⟩
    rcases h.2 with h2 | h2
    · exact Or.inl h2
    · right; simp only [h2]

/-! ## What the state knows: held payloads and dominated payloads -/

/-- The slot holds, at `pc`, a payload at or below `v`. -/
def Holds (k : Nat) (sl : Slot) (pc : Nat) (v : Payload) : Prop :=
  (sl.entry pc).stamp = sl.gen ∧ ple k ((sl.entry pc).start, (sl.entry pc).ctr) v

/-- `v` cannot beat the best candidate the state knows: exactly what `paPrune` rejects. -/
def Dom (k : Nat) (st : St) (v : Payload) : Prop := paPrune k st v.1 v.2 = true

theorem Dom_iff (k : Nat) (st : St) (v : Payload) :
    Dom k st v ↔ st.matched = true ∧ (st.so < v.1 ∨ (v.1 = st.so ∧ k ≠ 0 ∧ ctrLess st.bestCtr v.2 = true)) := by
  unfold Dom paPrune
  cases hm : st.matched
  · simp
  · by_cases h1 : st.so < v.1
    · simp [h1]
    · by_cases h2 : v.1 < st.so
      · simp [h1, h2]; omega
      · have hs : v.1 = st.so := by omega
        by_cases hk : k = 0
        · simp [h1, h2, hk]
        · simp [h1, h2, hk, hs]

/-- Domination is upward closed in the payload order. -/
theorem Dom_of_ple (k : Nat) (st : St) (v w : Payload) (hd : Dom k st v) (hle : ple k v w) : Dom k st w := by
  rw [Dom_iff] at hd ⊢
  obtain ⟨hm, hd⟩ := hd
  refine ⟨hm, ?_⟩
  rcases hle with hle | hle
  · simp only [plt, Bool.or_eq_true, Bool.and_eq_true, decide_eq_true_eq, beq_iff_eq, bne_iff_ne, ne_eq] at hle
    rcases hd with hd | ⟨h1, h2, h3⟩ <;> rcases hle with hl | ⟨⟨hl1, hl2⟩, hl3⟩
    · left; omega
    · left; omega
    · left; omega
    · right; exact ⟨by omega, h2, ctrLess_trans _ _ _ h3 hl3⟩
  · rcases hd with hd | ⟨h1, h2, h3⟩
    · left; rw [← hle.1]; exact hd
    · right
      refine ⟨hle.1 ▸ h1, h2, ?_⟩
      rcases hle.2 with h | h
      · exact absurd h h2
      · rw [← h]; exact h3

/-- Counters only grow, so a dominated payload stays dominated after a consuming step. -/
theorem Dom_bumpIf (p : Prog) (pc : Nat) (delta : Nat) (st : St) (v : Payload) (hd : Dom p.k st v) :
    Dom p.k st (v.1, bumpIf p pc v.2 delta) := by
  rw [Dom_iff] at hd ⊢
  obtain ⟨hm, hd⟩ := hd
  refine ⟨hm, ?_⟩
  rcases hd with hd | ⟨h1, h2, h3⟩
  · left; exact hd
  · right; exact ⟨h1, h2, ctrLess_bumpIf_right p pc delta _ _ h3⟩

/-- A held payload is still held after a consuming step by both. -/
theorem Holds_bumpIf_ple (p : Prog) (pc : Nat) (delta : Nat) (stored v : Payload) (h : ple p.k stored v) :
    ple p.k (stored.1, bumpIf p pc stored.2 delta) (v.1, bumpIf p pc v.2 delta) :=
  ple_bumpIf p pc delta stored v h

/-! ## What one store establishes, and what later stores preserve -/

/-- After a store of `v` at `pc`, either the slot holds `v` there or `v` was dominated. -/
theorem paStore_post (k : Nat) (st : St) (si pc : Nat) (v : Payload) (hpc : pc < (st.slot si).table.size)
    (hsi : si < st.slots.size)
    (hl : ((st.slot si).entry pc).stamp = (st.slot si).gen → ((st.slot si).entry pc).ctr.length = v.2.length) :
    Holds k ((paStore k st si pc v.1 v.2).2.slot si) pc v ∨ Dom k st v := by
  rcases paStore_cases k st si pc v.1 v.2 with h | ⟨h, hacc⟩
  · rw [h]
    unfold paStore at h
    split at h
    · right; assumption
    · split at h
      · simp at h
      · rename_i hnacc
        left
        unfold acceptsStore at hnacc
        simp only [Bool.or_eq_true, bne_iff_ne, ne_eq, not_or, Decidable.not_not] at hnacc
        refine ⟨hnacc.1, ?_⟩
        rw [betterThan_eq_plt] at hnacc
        simp only [Bool.not_eq_true] at hnacc
        exact ple_of_not_plt k _ v (hl hnacc.1) hnacc.2
  · rw [h]
    left
    unfold Holds
    have hslot : ((st.setSlot si (storeInto (st.slot si) pc v.1 v.2).1).charge
        (storeInto (st.slot si) pc v.1 v.2).2).slot si = (storeInto (st.slot si) pc v.1 v.2).1 := by
      simp only [St.slot_charge]; exact St.slot_setSlot_self _ _ _ hsi
    rw [hslot, storeInto_entry_self _ _ _ _ hpc, storeInto_gen]
    exact ⟨rfl, ple_refl k v⟩

/-- A store never makes a held payload worse: at `pc` only a better payload replaces it, elsewhere nothing moves. -/
theorem Holds_paStore (k : Nat) (st : St) (si pc q : Nat) (start : Nat) (ctr : List Nat) (v : Payload)
    (hsi : si < st.slots.size) (hq : q < (st.slot si).table.size) (h : Holds k (st.slot si) q v) :
    Holds k ((paStore k st si pc start ctr).2.slot si) q v := by
  rcases paStore_cases k st si pc start ctr with hc | ⟨hc, hacc⟩
  · rw [hc]; exact h
  · rw [hc]
    unfold Holds at h ⊢
    have hslot : ((st.setSlot si (storeInto (st.slot si) pc start ctr).1).charge
        (storeInto (st.slot si) pc start ctr).2).slot si = (storeInto (st.slot si) pc start ctr).1 := by
      simp only [St.slot_charge]; exact St.slot_setSlot_self _ _ _ hsi
    rw [hslot]
    by_cases hpq : pc = q
    · subst hpq
      rw [storeInto_entry_self _ _ _ _ hq, storeInto_gen]
      refine ⟨rfl, ?_⟩
      unfold acceptsStore at hacc
      simp only [Bool.or_eq_true, bne_iff_ne, ne_eq] at hacc
      rcases hacc with hacc | hacc
      · exact absurd h.1 hacc
      · rw [betterThan_eq_plt] at hacc
        exact ple_trans k _ _ v (Or.inl hacc) h.2
    · rw [storeInto_entry_ne _ _ _ _ _ hpq, storeInto_gen]
      exact h

theorem Holds_other_slot (k : Nat) (st : St) (si sj pc : Nat) (start : Nat) (ctr : List Nat) (v : Payload)
    (hne : si ≠ sj) (h : Holds k (st.slot sj) pc v) :
    Holds k ((paStore k st si pc start ctr).2.slot sj) pc v := by
  rcases paStore_cases k st si pc start ctr with hc | ⟨hc, _⟩
  · rw [hc]; exact h
  · rw [hc]; simp only [St.slot_charge]; rw [St.slot_setSlot_ne _ _ _ _ hne]; exact h

/-! ## The candidate update -/

def bestOf (st : St) : Nat × List Nat × Nat := (st.so, st.bestCtr, st.eo)

theorem selLE_refl (a : Nat × List Nat × Nat) : selLE a a := by
  simp [selLE]

theorem selLE_trans (a b c : Nat × List Nat × Nat) (h1 : selLE a b) (h2 : selLE b c) : selLE a c := by
  simp only [selLE] at *
  rcases h1 with h1 | ⟨h1, h1'⟩ <;> rcases h2 with h2 | ⟨h2, h2'⟩
  · left; omega
  · left; omega
  · left; omega
  · right
    refine ⟨h1.trans h2, ?_⟩
    rcases h1' with h1' | ⟨h1', h1''⟩ <;> rcases h2' with h2' | ⟨h2', h2''⟩
    · left; exact ctrLess_trans _ _ _ h1' h2'
    · left; rw [← h2']; exact h1'
    · left; rw [h1']; exact h2'
    · right; exact ⟨h1'.trans h2', Nat.le_trans h2'' h1''⟩

/-- What the candidate update does to the best: the new best is the candidate or the old best, whichever the
selection order puts first, and it is at or before both. -/
theorem paConsider_spec (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat)
    (hc : ctr.length = k) (hb : st.bestCtr.length = k) :
    let st' := paConsider k st start ctr e
    st'.matched = true ∧
    (bestOf st' = (start, ctr, e) ∨ (st.matched = true ∧ bestOf st' = bestOf st)) ∧
    selLE (bestOf st') (start, ctr, e) ∧
    (st.matched = true → selLE (bestOf st') (bestOf st)) := by
  intro st'
  by_cases hm : st.matched = true
  · by_cases h1 : start < st.so
    · simp [st', paConsider, considerCore, bestOf, hm, h1, selLE]
    · by_cases h2 : st.so < start
      · simp [st', paConsider, considerCore, bestOf, hm, h1, h2, selLE]
      · have hs : start = st.so := by omega
        subst hs
        by_cases hk : k = 0
        · subst hk
          have hc0 : ctr = [] := List.eq_nil_of_length_eq_zero hc
          have hb0 : st.bestCtr = [] := List.eq_nil_of_length_eq_zero hb
          by_cases he : st.eo < e
          · simp [st', paConsider, considerCore, bestOf, hm, h1, h2, he, hc0, hb0, selLE, ctrLess]; omega
          · simp [st', paConsider, considerCore, bestOf, hm, h1, h2, he, hc0, hb0, selLE, ctrLess]; omega
        · have hk' : 0 < k := by omega
          rcases ctrLess_total ctr st.bestCtr (hc.trans hb.symm) with h3 | h3 | h3
          · have h4 := ctrLess_asymm _ _ h3
            simp [st', paConsider, considerCore, bestOf, hm, h1, h2, hk', h3, h4, selLE]
          · have h4 := ctrLess_asymm _ _ h3
            simp [st', paConsider, considerCore, bestOf, hm, h1, h2, hk', h3, h4, selLE]
          · subst h3
            have h4 := ctrLess_irrefl st.bestCtr
            by_cases he : st.eo < e
            · simp [st', paConsider, considerCore, bestOf, hm, h1, h2, hk', h4, he, selLE]; omega
            · simp [st', paConsider, considerCore, bestOf, hm, h1, h2, hk', h4, he, selLE]; omega
  · simp [st', paConsider, considerCore, bestOf, hm, selLE]

/-- The best only improves, so every domination survives a candidate update. -/
theorem Dom_paConsider (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) (v : Payload)
    (hd : Dom k st v) (hc : ctr.length = k) (hb : st.bestCtr.length = k) :
    Dom k (paConsider k st start ctr e) v := by
  obtain ⟨hm', hbest, _, himp⟩ := paConsider_spec k st start ctr e hc hb
  rw [Dom_iff] at hd
  obtain ⟨hm, hd⟩ := hd
  have hsel := himp hm
  rw [Dom_iff]
  refine ⟨hm', ?_⟩
  simp only [bestOf, selLE] at hsel
  rcases hsel with hsel | ⟨hsel, hsel'⟩
  · left
    rcases hd with hd | ⟨hd, _, _⟩ <;> omega
  · rcases hd with hd | ⟨hd1, hd2, hd3⟩
    · left; omega
    · right
      refine ⟨by omega, hd2, ?_⟩
      rcases hsel' with h | ⟨h, _⟩
      · exact ctrLess_trans _ _ _ h hd3
      · rw [h]; exact hd3

theorem paConsider_bestCtr_length (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat)
    (hc : ctr.length = k) (hb : st.bestCtr.length = k) : (paConsider k st start ctr e).bestCtr.length = k := by
  obtain ⟨_, hbest, _, _⟩ := paConsider_spec k st start ctr e hc hb
  rcases hbest with h | ⟨_, h⟩
  · simp only [bestOf, Prod.mk.injEq] at h; rw [h.2.1]; exact hc
  · simp only [bestOf, Prod.mk.injEq] at h; rw [h.2.1]; exact hb

/-! ## The closure invariant -/

def Fresh (sl : Slot) (pc : Nat) : Prop := (sl.entry pc).stamp = sl.gen
def payloadAt (sl : Slot) (pc : Nat) : Payload := ((sl.entry pc).start, (sl.entry pc).ctr)

/-- The thread of a slot entry at a boundary. -/
def thOf (pos : Nat) (sl : Slot) (pc : Nat) : Th := ⟨pos, pc, (sl.entry pc).start, (sl.entry pc).ctr⟩

/-- The invariant of one slot table during the run. -/
structure RSlot (p : Prog) (sl : Slot) : Prop where
  size : sl.table.size = p.n
  lens : ∀ pc, pc < p.n → (sl.entry pc).ctr.length = p.k
  stampLe : ∀ pc, pc < p.n → (sl.entry pc).stamp ≤ sl.gen
  activeLt : ∀ pc ∈ sl.active, pc < p.n ∧ Fresh sl pc
  freshActive : ∀ pc, pc < p.n → Fresh sl pc → pc ∈ sl.active

/-- The invariant of a slot that may still be behind its next generation `g`: the table shape, the stamp
bound, and the full invariant once the slot is current. A zeroed slot satisfies it for every `g ≥ 1`. -/
structure RSlotQ (p : Prog) (sl : Slot) (g : Nat) : Prop where
  size : sl.table.size = p.n
  lens : ∀ pc, pc < p.n → (sl.entry pc).ctr.length = p.k
  stampLe : ∀ pc, pc < p.n → (sl.entry pc).stamp ≤ sl.gen
  genLe : sl.gen ≤ g
  cur : sl.gen = g → RSlot p sl

theorem RSlotQ.ofRSlot (p : Prog) (sl : Slot) (g : Nat) (h : RSlot p sl) (hg : sl.gen ≤ g) : RSlotQ p sl g :=
  ⟨h.size, h.lens, h.stampLe, hg, fun _ => h⟩


theorem RSlot_storeInto (p : Prog) (sl : Slot) (pc start : Nat) (ctr : List Nat) (h : RSlot p sl) (hpc : pc < p.n)
    (hl : ctr.length = p.k) : RSlot p (storeInto sl pc start ctr).1 := by
  have hsize := h.size
  have hself := storeInto_entry_self sl pc start ctr (hsize ▸ hpc)
  have hother := fun q hq => storeInto_entry_ne sl pc q start ctr hq
  have hgen := storeInto_gen sl pc start ctr
  have hact := storeInto_active sl pc start ctr
  refine ⟨by rw [storeInto_size]; exact hsize, ?_, ?_, ?_, ?_⟩
  · intro q hq
    by_cases hpq : pc = q
    · subst hpq; rw [hself]; exact hl
    · rw [hother q hpq]; exact h.lens q hq
  · intro q hq
    rw [hgen]
    by_cases hpq : pc = q
    · subst hpq; rw [hself]; exact Nat.le_refl _
    · rw [hother q hpq]; exact h.stampLe q hq
  · intro q hq
    rw [hact] at hq
    unfold Fresh
    rw [hgen]
    split at hq
    · rename_i hf
      obtain ⟨h1, h2⟩ := h.activeLt q hq
      refine ⟨h1, ?_⟩
      by_cases hpq : pc = q
      · subst hpq; rw [hself]
      · rw [hother q hpq]; exact h2
    · simp only [List.mem_append, List.mem_singleton] at hq
      rcases hq with hq | hq
      · obtain ⟨h1, h2⟩ := h.activeLt q hq
        refine ⟨h1, ?_⟩
        by_cases hpq : pc = q
        · subst hpq; rw [hself]
        · rw [hother q hpq]; exact h2
      · subst hq; rw [hself]; exact ⟨hpc, rfl⟩
  · intro q hq hf
    unfold Fresh at hf
    rw [hgen] at hf
    rw [hact]
    split
    · rename_i hfr
      by_cases hpq : pc = q
      · subst hpq
        exact h.freshActive _ hq (by unfold Fresh; simpa using hfr)
      · rw [hother q hpq] at hf
        exact h.freshActive q hq hf
    · rename_i hfr
      simp only [List.mem_append, List.mem_singleton]
      by_cases hpq : pc = q
      · right; exact hpq.symm
      · left
        rw [hother q hpq] at hf
        exact h.freshActive q hq hf

theorem RSlot_paStore (k : Nat) (p : Prog) (st : St) (si pc start : Nat) (ctr : List Nat) (h : RSlot p (st.slot si))
    (hsi : si < st.slots.size) (hpc : pc < p.n) (hl : ctr.length = p.k) :
    RSlot p ((paStore k st si pc start ctr).2.slot si) := by
  rcases paStore_cases k st si pc start ctr with hc | ⟨hc, _⟩
  · rw [hc]; exact h
  · rw [hc]
    simp only [St.slot_charge]
    rw [St.slot_setSlot_self _ _ _ hsi]
    exact RSlot_storeInto p _ pc start ctr h hpc hl

theorem RSlot_paRelax (p : Prog) (st : St) (si pc start : Nat) (ctr : List Nat) (h : RSlot p (st.slot si))
    (hsi : si < st.slots.size) (hpc : pc < p.n) (hl : ctr.length = p.k) :
    RSlot p ((paRelax p.k st si pc start ctr).slot si) := by
  simp only [paRelax]
  have := RSlot_paStore p.k p { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr h
    (by simpa using hsi) hpc hl
  cases hst : (paStore p.k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr).1
  · simpa using this
  · simp only [ite_true, St.slot_pushQueue]; exact this


/-- What a popped instruction owes for the payload it was popped with: every epsilon successor holds a payload
at least as good or the payload is dominated, and an accept has been offered to the best. -/
def Done (p : Prog) (input : Input) (pos si : Nat) (st : St) (pc : Nat) (v : Payload) : Prop :=
  (∀ q, EpsEdge p input pos pc q → Holds p.k (st.slot si) q v ∨ Dom p.k st v) ∧
  (p.op pc = .accept → st.matched = true ∧ selLE (bestOf st) (v.1, v.2, chainPos input pos))

/-- The closure invariant at boundary `pos` on slot `si`. `except` names an instruction being handled. -/
structure CInvX (p : Prog) (atoms : Atoms) (input : Input) (pos si : Nat) (st : St) (except : Option Nat) :
    Prop where
  slotsSize : st.slots.size = p.ring
  siLt : si < p.ring
  tableSize : (st.slot si).table.size = p.n
  pos_eq : st.pos = chainPos input pos
  bol_eq : st.bol = true ↔ bolRef input pos
  eol_eq : st.eol = true ↔ eolRef input pos
  queueLt : ∀ pc ∈ st.queue, pc < p.n
  bestLen : st.bestCtr.length = p.k
  lens : ∀ pc, pc < p.n → ((st.slot si).entry pc).ctr.length = p.k
  sound : ∀ pc, pc < p.n → Fresh (st.slot si) pc → Reach p atoms input (thOf pos (st.slot si) pc)
  queued : ∀ pc, pc < p.n → Fresh (st.slot si) pc → some pc ≠ except →
    pc ∈ st.queue ∨ Done p input pos si st pc (payloadAt (st.slot si) pc)
  bestCand : st.matched = true → Cand p atoms input st.so st.bestCtr st.eo
  rslot : RSlot p (st.slot si)

/-- Facts that a relaxation leaves alone. -/
theorem paRelax_bestOf (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    bestOf (paRelax k st si pc start ctr) = bestOf st ∧ (paRelax k st si pc start ctr).matched = st.matched ∧
    (paRelax k st si pc start ctr).pos = st.pos ∧ (paRelax k st si pc start ctr).bol = st.bol ∧
    (paRelax k st si pc start ctr).eol = st.eol ∧ (paRelax k st si pc start ctr).bestCtr = st.bestCtr := by
  simp only [paRelax]
  rcases paStore_cases k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr with
    h | ⟨h, _⟩ <;> rw [h] <;> simp [bestOf, St.pushQueue, St.charge, St.setSlot]

theorem paStore_bestOf (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    bestOf (paStore k st si pc start ctr).2 = bestOf st ∧ (paStore k st si pc start ctr).2.matched = st.matched := by
  rcases paStore_cases k st si pc start ctr with h | ⟨h, _⟩ <;> rw [h] <;> simp [bestOf, St.charge, St.setSlot]

/-- A store changes at most the entry it stores at. -/
theorem paStore_entries (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) (hsi : si < st.slots.size) :
    let st' := (paStore k st si pc start ctr).2
    (st'.slot si).gen = (st.slot si).gen ∧ (st'.slot si).table.size = (st.slot si).table.size ∧
    (∀ q, q ≠ pc → (st'.slot si).entry q = (st.slot si).entry q) ∧
    ((st'.slot si).entry pc = (st.slot si).entry pc ∨
      ((st'.slot si).entry pc = { stamp := (st.slot si).gen, start, ctr } ∧ (paStore k st si pc start ctr).1 = true)) ∧
    (∀ sj, sj ≠ si → st'.slot sj = st.slot sj) ∧ st'.slots.size = st.slots.size ∧ st'.queue = st.queue := by
  intro st'
  rcases paStore_cases k st si pc start ctr with h | ⟨h, _⟩
  · simp [st', h]
  · have hslot : st'.slot si = (storeInto (st.slot si) pc start ctr).1 := by
      simp only [st', h, St.slot_charge]; exact St.slot_setSlot_self _ _ _ hsi
    have hother : ∀ sj, sj ≠ si → st'.slot sj = st.slot sj := by
      intro sj hsj
      simp only [st', h, St.slot_charge]; exact St.slot_setSlot_ne _ _ _ _ (Ne.symm hsj)
    refine ⟨by rw [hslot, storeInto_gen], by rw [hslot, storeInto_size],
      fun q hq => by rw [hslot, storeInto_entry_ne _ _ _ _ _ (Ne.symm hq)], ?_, hother,
      by simp [st', h], by simp [st', h]⟩
    by_cases hpc : pc < (st.slot si).table.size
    · right; exact ⟨by rw [hslot, storeInto_entry_self _ _ _ _ hpc], by rw [h]⟩
    · left
      rw [hslot]
      simp only [Slot.entry_eq]
      unfold storeInto
      split <;> simp [Array.getElem?_setIfInBounds, hpc]

/-- A relaxation changes at most the entry it stores at, and then it queues that instruction. -/
theorem paRelax_entries (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) (hsi : si < st.slots.size) :
    let st' := paRelax k st si pc start ctr
    (st'.slot si).gen = (st.slot si).gen ∧ (st'.slot si).table.size = (st.slot si).table.size ∧
    (∀ q, q ≠ pc → (st'.slot si).entry q = (st.slot si).entry q) ∧
    ((st'.slot si).entry pc = (st.slot si).entry pc ∨
      ((st'.slot si).entry pc = { stamp := (st.slot si).gen, start, ctr } ∧ pc ∈ st'.queue)) ∧
    (∀ q ∈ st.queue, q ∈ st'.queue) ∧ (∀ sj, sj ≠ si → st'.slot sj = st.slot sj) ∧ st'.slots.size = st.slots.size ∧
    (st'.queue = st.queue ∨ st'.queue = pc :: st.queue) := by
  intro st'
  have st0 : St := { st with m := { st.m with relaxes := st.m.relaxes + 1 } }
  obtain ⟨h1, h2, h3, h4, h5, h6, h7⟩ := paStore_entries k { st with m := { st.m with relaxes := st.m.relaxes + 1 } }
    si pc start ctr (by simpa using hsi)
  simp only [St.slot_mk_m, St.slots_mk_m] at h1 h2 h3 h4 h5 h6
  have hq0 : ({ st with m := { st.m with relaxes := st.m.relaxes + 1 } } : St).queue = st.queue := rfl
  rw [hq0] at h7
  simp only [st', paRelax]
  cases hstayed : (paStore k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr).1
  · simp only [Bool.false_eq_true, ite_false]
    refine ⟨h1, h2, h3, ?_, fun q hq => by rw [h7]; exact hq, h5, h6, Or.inl h7⟩
    rcases h4 with h4 | ⟨_, h4⟩
    · exact Or.inl h4
    · rw [hstayed] at h4; exact absurd h4 (by simp)
  · simp only [ite_true, St.slot_pushQueue, St.queue_pushQueue, St.slots_pushQueue]
    refine ⟨h1, h2, h3, ?_, fun q hq => List.mem_cons_of_mem _ (by rw [h7]; exact hq), h5, h6, Or.inr (by rw [h7])⟩
    rcases h4 with h4 | ⟨h4, _⟩
    · exact Or.inl h4
    · right; exact ⟨h4, by simp⟩

/-! ## Reach under epsilon edges, and what a relaxation establishes -/

theorem Reach_eps (p : Prog) (atoms : Atoms) (input : Input) (pos pc q s : Nat) (c : List Nat)
    (h : Reach p atoms input ⟨pos, pc, s, c⟩) (he : EpsEdge p input pos pc q) : Reach p atoms input ⟨pos, q, s, c⟩ := by
  obtain ⟨q0, hv, hs⟩ := h
  exact ⟨q0, hv, Steps.tail _ _ _ hs (Step.eps ⟨pos, pc, s, c⟩ q he)⟩

/-- Every thread's counters have `k` entries. -/
theorem Steps_len (p : Prog) (atoms : Atoms) (input : Input) (a b : Th) (h : Steps p atoms input a b) :
    b.c.length = a.c.length := by
  induction h with
  | refl => rfl
  | tail b c _ s ih =>
    cases s with
    | eps q _ => exact ih
    | consume delta _ => simp only [bumpIf_length]; exact ih

theorem Reach_len (p : Prog) (atoms : Atoms) (input : Input) (T : Th) (h : Reach p atoms input T) :
    T.c.length = p.k := by
  obtain ⟨q0, _, hs⟩ := h
  rw [Steps_len p atoms input _ _ hs]
  simp [spawnTh]

/-- Every thread sits inside the program. -/
theorem Steps_pc (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (a b : Th) (h : Steps p atoms input a b)
    (ha : a.pc < p.n) : b.pc < p.n := by
  induction h with
  | refl => exact ha
  | tail b c _ s ih =>
    have hb := ih
    cases s with
    | eps q he =>
      simp only [EpsEdge, Prog.op, Prog.next, Prog.alt] at he
      have h1 := (hwf.2 b.pc hb).1
      have h2 := (hwf.2 b.pc hb).2
      split at he
      · rcases he with he | he <;> subst he <;> assumption
      · subst he; exact h1
      · rw [he.2]; exact h1
      · rw [he.2]; exact h1
      · exact absurd he id
    | consume delta _ =>
      exact (hwf.2 b.pc hb).1

theorem Reach_pc (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (T : Th) (h : Reach p atoms input T) :
    T.pc < p.n := by
  obtain ⟨q0, _, hs⟩ := h
  exact Steps_pc p atoms input hwf _ _ hs hwf.1

theorem EpsEdge_lt (p : Prog) (input : Input) (hwf : p.wf) (pos pc q : Nat) (hpc : pc < p.n)
    (he : EpsEdge p input pos pc q) : q < p.n := by
  have h1 := (hwf.2 pc hpc).1
  have h2 := (hwf.2 pc hpc).2
  simp only [EpsEdge, Prog.op, Prog.next, Prog.alt] at he h1 h2
  split at he
  · rcases he with he | he <;> subst he <;> assumption
  · subst he; exact h1
  · rw [he.2]; exact h1
  · rw [he.2]; exact h1
  · exact absurd he id

/-! Persistence through a relaxation. -/

theorem Dom_paRelax (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) (v : Payload) (h : Dom k st v) :
    Dom k (paRelax k st si pc start ctr) v := by
  obtain ⟨hb, hm, _⟩ := paRelax_bestOf k st si pc start ctr
  rw [Dom_iff] at h ⊢
  simp only [bestOf, Prod.mk.injEq] at hb
  rw [hm, hb.1, hb.2.1]
  exact h

theorem Holds_paRelax (k : Nat) (st : St) (si pc q start : Nat) (ctr : List Nat) (v : Payload)
    (hsi : si < st.slots.size) (hq : q < (st.slot si).table.size) (h : Holds k (st.slot si) q v) :
    Holds k ((paRelax k st si pc start ctr).slot si) q v := by
  simp only [paRelax]
  have h0 : Holds k (({ st with m := { st.m with relaxes := st.m.relaxes + 1 } } : St).slot si) q v := h
  have := Holds_paStore k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc q start ctr v
    (by simpa using hsi) hq h0
  cases hst : (paStore k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr).1
  · simpa using this
  · simp only [ite_true, St.slot_pushQueue]; exact this

theorem Done_paRelax (p : Prog) (input : Input) (hwf : p.wf) (pos si : Nat) (st : St) (pc r start : Nat)
    (ctr : List Nat) (v : Payload) (hsi : si < st.slots.size) (htab : (st.slot si).table.size = p.n) (hr : r < p.n)
    (h : Done p input pos si st r v) : Done p input pos si (paRelax p.k st si pc start ctr) r v := by
  obtain ⟨hb, hm, _⟩ := paRelax_bestOf p.k st si pc start ctr
  refine ⟨fun q hq => ?_, fun ha => ?_⟩
  · rcases h.1 q hq with h1 | h1
    · left; exact Holds_paRelax p.k st si pc q start ctr v hsi (htab ▸ EpsEdge_lt p input hwf pos r q hr hq) h1
    · right; exact Dom_paRelax p.k st si pc start ctr v h1
  · rw [hm, hb]; exact h.2 ha

/-! Persistence through a candidate update. -/

theorem Holds_paConsider (k : Nat) (st : St) (si q start : Nat) (ctr : List Nat) (e : Nat) (v : Payload)
    (h : Holds k (st.slot si) q v) : Holds k ((paConsider k st start ctr e).slot si) q v := by
  rw [paConsider_slot]; exact h

theorem Done_paConsider (p : Prog) (input : Input) (pos si : Nat) (st : St) (r start : Nat) (ctr : List Nat)
    (v : Payload) (hc : ctr.length = p.k) (hb : st.bestCtr.length = p.k) (h : Done p input pos si st r v) :
    Done p input pos si (paConsider p.k st start ctr (chainPos input pos)) r v := by
  obtain ⟨hm', _, _, himp⟩ := paConsider_spec p.k st start ctr (chainPos input pos) hc hb
  refine ⟨fun q hq => ?_, fun ha => ?_⟩
  · rcases h.1 q hq with h1 | h1
    · left; exact Holds_paConsider _ _ _ _ _ _ _ _ h1
    · right; exact Dom_paConsider p.k st start ctr (chainPos input pos) v h1 hc hb
  · obtain ⟨hm, hs⟩ := h.2 ha
    exact ⟨hm', selLE_trans _ _ _ (himp hm) hs⟩

/-- One relaxation of `v` at `q`: afterwards the slot holds `v` at `q`, or `v` was dominated. -/
theorem paRelax_post (k : Nat) (st : St) (si q : Nat) (v : Payload) (hsi : si < st.slots.size)
    (hq : q < (st.slot si).table.size)
    (hl : ((st.slot si).entry q).stamp = (st.slot si).gen → ((st.slot si).entry q).ctr.length = v.2.length) :
    Holds k ((paRelax k st si q v.1 v.2).slot si) q v ∨ Dom k st v := by
  simp only [paRelax]
  have := paStore_post k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si q v hq (by simpa using hsi) hl
  simp only [Dom_iff] at this ⊢
  cases hst : (paStore k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si q v.1 v.2).1
  · simpa using this
  · simp only [ite_true, St.slot_pushQueue]; exact this

/-! ## Handling one popped instruction -/

theorem CInvX.slot_lt (h : CInvX p atoms input pos si st ex) : si < st.slots.size := by
  rw [h.slotsSize]; exact h.siLt

/-- One relaxation of a payload `w` that `pc` carries at this boundary, along an edge to `q`, while `pc` is being
handled. -/
theorem relax_transition (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (pos si : Nat) (st : St) (pc q : Nat)
    (w : Payload) (hinv : CInvX p atoms input pos si st (some pc)) (hpc : pc < p.n)
    (hw : Reach p atoms input ⟨pos, pc, w.1, w.2⟩) (hwl : w.2.length = p.k) (hedge : EpsEdge p input pos pc q) :
    let st' := paRelax p.k st si q w.1 w.2
    CInvX p atoms input pos si st' (some pc) ∧ (Holds p.k (st'.slot si) q w ∨ Dom p.k st' w) ∧
    bestOf st' = bestOf st ∧ st'.matched = st.matched ∧
    ((st'.slot si).entry pc = (st.slot si).entry pc ∨ pc ∈ st'.queue) ∧
    (∀ r w', r < p.n → Holds p.k (st.slot si) r w' → Holds p.k (st'.slot si) r w') ∧
    (∀ w', Dom p.k st w' → Dom p.k st' w') ∧ (∀ r, r ∈ st.queue → r ∈ st'.queue) ∧
    (st'.slot si).gen = (st.slot si).gen := by
  intro st'
  have hq : q < p.n := EpsEdge_lt p input hwf pos pc q hpc hedge
  have hsi := hinv.slot_lt
  have htab := hinv.tableSize
  obtain ⟨hgen, hsize, hother, hself, hqueue, hslots, hsz, hqshape⟩ := paRelax_entries p.k st si q w.1 w.2 hsi
  obtain ⟨hbest, hm, hpos, hbol, heol, hbc⟩ := paRelax_bestOf p.k st si q w.1 w.2
  have hpost : Holds p.k (st'.slot si) q w ∨ Dom p.k st' w := by
    rcases paRelax_post p.k st si q w hsi (htab ▸ hq) (fun _ => by rw [hinv.lens q hq]; exact hwl.symm) with h | h
    · exact Or.inl h
    · exact Or.inr (Dom_paRelax p.k st si q w.1 w.2 w h)
  have hentry : ∀ r, (st'.slot si).entry r = (st.slot si).entry r ∨
      (r = q ∧ (st'.slot si).entry r = { stamp := (st.slot si).gen, start := w.1, ctr := w.2 } ∧ r ∈ st'.queue) := by
    intro r
    by_cases hrq : r = q
    · subst hrq
      rcases hself with h | ⟨h, hmem⟩
      · exact Or.inl h
      · exact Or.inr ⟨rfl, h, hmem⟩
    · exact Or.inl (hother r hrq)
  refine ⟨?_, hpost, hbest, hm, ?_, ?_, ?_, hqueue, hgen⟩
  · refine ⟨by rw [hsz]; exact hinv.slotsSize, hinv.siLt, by rw [hsize]; exact hinv.tableSize,
      by rw [hpos]; exact hinv.pos_eq, by rw [hbol]; exact hinv.bol_eq, by rw [heol]; exact hinv.eol_eq, ?_,
      by rw [hbc]; exact hinv.bestLen, ?_, ?_, ?_, ?_,
      RSlot_paRelax p st si q w.1 w.2 hinv.rslot hsi hq hwl⟩
    · intro r hr
      rcases hqshape with h | h
      · rw [h] at hr; exact hinv.queueLt r hr
      · rw [h] at hr
        rcases List.mem_cons.mp hr with hr | hr
        · subst hr; exact hq
        · exact hinv.queueLt r hr
    · intro r hr
      rcases hentry r with h | ⟨_, h, _⟩
      · rw [h]; exact hinv.lens r hr
      · rw [h]; exact hwl
    · intro r hr hf
      rcases hentry r with h | ⟨hrq, h, _⟩
      · unfold Fresh at hf; rw [h, hgen] at hf
        have := hinv.sound r hr hf
        unfold thOf at this ⊢; rw [h]; exact this
      · unfold thOf; rw [h, hrq]
        exact Reach_eps p atoms input pos pc q w.1 w.2 hw hedge
    · intro r hr hf hne
      rcases hentry r with h | ⟨hrq, h, hmem⟩
      · unfold Fresh at hf; rw [h, hgen] at hf
        rcases hinv.queued r hr hf hne with hmem | hdone
        · exact Or.inl (hqueue r hmem)
        · right
          unfold payloadAt; rw [h]
          exact Done_paRelax p input hwf pos si st q r w.1 w.2 _ hsi htab hr hdone
      · exact Or.inl hmem
    · intro hm'
      rw [hm] at hm'
      have := hinv.bestCand hm'
      simp only [bestOf, Prod.mk.injEq] at hbest
      rw [hbest.1, hbest.2.1, hbest.2.2]; exact this
  · rcases hentry pc with h | ⟨_, _, hmem⟩
    · exact Or.inl h
    · exact Or.inr hmem
  · intro r w' hr hh
    exact Holds_paRelax p.k st si q r w.1 w.2 w' hsi (htab ▸ hr) hh
  · intro w' hw'
    exact Dom_paRelax p.k st si q w.1 w.2 w' hw'

/-- Dropping the exception once its instruction is queued or done. -/
theorem CInvX.close (h : CInvX p atoms input pos si st (some pc))
    (hpc : Fresh (st.slot si) pc → pc ∈ st.queue ∨ Done p input pos si st pc (payloadAt (st.slot si) pc)) :
    CInvX p atoms input pos si st none :=
  ⟨h.slotsSize, h.siLt, h.tableSize, h.pos_eq, h.bol_eq, h.eol_eq, h.queueLt, h.bestLen, h.lens, h.sound,
   fun r hr hf _ => by
     by_cases hrpc : r = pc
     · subst hrpc; exact hpc hf
     · exact h.queued r hr hf (by simpa using hrpc),
   h.bestCand, h.rslot⟩

theorem CInvX.except (h : CInvX p atoms input pos si st none) (pc : Nat) : CInvX p atoms input pos si st (some pc) :=
  ⟨h.slotsSize, h.siLt, h.tableSize, h.pos_eq, h.bol_eq, h.eol_eq, h.queueLt, h.bestLen, h.lens, h.sound,
   fun r hr hf _ => h.queued r hr hf (by simp), h.bestCand, h.rslot⟩

theorem paConsider_flags (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (paConsider k st start ctr e).bol = st.bol ∧ (paConsider k st start ctr e).eol = st.eol := by
  simp only [paConsider]; unfold considerCore
  split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> exact ⟨rfl, rfl⟩

/-- Handling the fresh instruction `pc` with the payload it was popped with. -/
theorem handleOp_correct (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (pos si : Nat) (st : St) (pc : Nat)
    (hinv : CInvX p atoms input pos si st (some pc)) (hfresh : Fresh (st.slot si) pc) (hpc : pc < p.n) :
    let v := payloadAt (st.slot si) pc
    let st' := handleOp p si st pc v.1 v.2
    CInvX p atoms input pos si st' none ∧
    (st.matched = true → st'.matched = true ∧ selLE (bestOf st') (bestOf st)) := by
  intro v st'
  have hReach : Reach p atoms input ⟨pos, pc, v.1, v.2⟩ := hinv.sound pc hpc hfresh
  have hlen : v.2.length = p.k := hinv.lens pc hpc
  have hpos := hinv.pos_eq
  -- The edges, by opcode.
  have hedge_split : p.op pc = .split → EpsEdge p input pos pc (p.ins.getD pc default).next ∧
      EpsEdge p input pos pc (p.ins.getD pc default).alt := by
    intro h; simp [EpsEdge, h, Prog.next, Prog.alt]
  have hedge_jmp : p.op pc = .jmp → EpsEdge p input pos pc (p.ins.getD pc default).next := by
    intro h; simp [EpsEdge, h, Prog.next]
  have hedge_bol : p.op pc = .bol → bolRef input pos →
      EpsEdge p input pos pc (p.ins.getD pc default).next := by
    intro h hb; simp [EpsEdge, h, hb, Prog.next]
  have hedge_eol : p.op pc = .eol → eolRef input pos →
      EpsEdge p input pos pc (p.ins.getD pc default).next := by
    intro h he; simp [EpsEdge, h, he, Prog.next]
  simp only [st', handleOp]
  split
  · -- split: two relaxations
    rename_i hop
    have hop' : p.op pc = .split := hop
    obtain ⟨he1, he2⟩ := hedge_split hop'
    obtain ⟨hi1, hpost1, hb1, hm1, hself1, hholds1, hdom1, hq1, hg1⟩ :=
      relax_transition p atoms input hwf pos si st pc _ v hinv hpc hReach hlen he1
    obtain ⟨hi2, hpost2, hb2, hm2, hself2, hholds2, hdom2, hq2, hg2⟩ :=
      relax_transition p atoms input hwf pos si _ pc _ v hi1 hpc hReach hlen he2
    refine ⟨hi2.close fun _ => ?_, fun hm => ⟨by rw [hm2, hm1, hm], by rw [hb2, hb1]; exact selLE_refl _⟩⟩
    rcases hself2 with hs2 | hs2
    · rcases hself1 with hs1 | hs1
      · right
        unfold payloadAt; rw [hs2, hs1]
        refine ⟨fun q hq => ?_, fun ha => by rw [hop'] at ha; cases ha⟩
        simp only [EpsEdge, hop'] at hq
        rcases hq with hq | hq <;> subst hq
        · rcases hpost1 with h | h
          · left; exact hholds2 _ _ (EpsEdge_lt p input hwf pos pc _ hpc he1) h
          · right; exact hdom2 _ h
        · exact hpost2
      · left; exact hq2 _ hs1
    · left; exact hs2
  · -- jmp
    rename_i hop
    have hop' : p.op pc = .jmp := hop
    have he := hedge_jmp hop'
    obtain ⟨hi1, hpost1, hb1, hm1, hself1, _, _, _, _⟩ :=
      relax_transition p atoms input hwf pos si st pc _ v hinv hpc hReach hlen he
    refine ⟨hi1.close fun _ => ?_, fun hm => ⟨by rw [hm1, hm], by rw [hb1]; exact selLE_refl _⟩⟩
    rcases hself1 with hs1 | hs1
    · right
      unfold payloadAt; rw [hs1]
      refine ⟨fun q hq => ?_, fun ha => by rw [hop'] at ha; cases ha⟩
      simp only [EpsEdge, hop'] at hq
      subst hq; exact hpost1
    · left; exact hs1
  · -- bol
    rename_i hop
    have hop' : p.op pc = .bol := hop
    by_cases hb : st.bol = true
    · rw [if_pos hb]
      have he := hedge_bol hop' (hinv.bol_eq.mp hb)
      obtain ⟨hi1, hpost1, hb1, hm1, hself1, _, _, _, _⟩ :=
        relax_transition p atoms input hwf pos si st pc _ v hinv hpc hReach hlen he
      refine ⟨hi1.close fun _ => ?_, fun hm => ⟨by rw [hm1, hm], by rw [hb1]; exact selLE_refl _⟩⟩
      rcases hself1 with hs1 | hs1
      · right
        unfold payloadAt; rw [hs1]
        refine ⟨fun q hq => ?_, fun ha => by rw [hop'] at ha; cases ha⟩
        simp only [EpsEdge, hop'] at hq
        rw [hq.2]; exact hpost1
      · left; exact hs1
    · rw [if_neg hb]
      refine ⟨hinv.close fun _ => Or.inr ⟨fun q hq => ?_, fun ha => by rw [hop'] at ha; cases ha⟩,
        fun hm => ⟨hm, selLE_refl _⟩⟩
      simp only [EpsEdge, hop'] at hq
      exact absurd (hinv.bol_eq.mpr hq.1) hb
  · -- eol
    rename_i hop
    have hop' : p.op pc = .eol := hop
    by_cases hb : st.eol = true
    · rw [if_pos hb]
      have he := hedge_eol hop' (hinv.eol_eq.mp hb)
      obtain ⟨hi1, hpost1, hb1, hm1, hself1, _, _, _, _⟩ :=
        relax_transition p atoms input hwf pos si st pc _ v hinv hpc hReach hlen he
      refine ⟨hi1.close fun _ => ?_, fun hm => ⟨by rw [hm1, hm], by rw [hb1]; exact selLE_refl _⟩⟩
      rcases hself1 with hs1 | hs1
      · right
        unfold payloadAt; rw [hs1]
        refine ⟨fun q hq => ?_, fun ha => by rw [hop'] at ha; cases ha⟩
        simp only [EpsEdge, hop'] at hq
        rw [hq.2]; exact hpost1
      · left; exact hs1
    · rw [if_neg hb]
      refine ⟨hinv.close fun _ => Or.inr ⟨fun q hq => ?_, fun ha => by rw [hop'] at ha; cases ha⟩,
        fun hm => ⟨hm, selLE_refl _⟩⟩
      simp only [EpsEdge, hop'] at hq
      exact absurd (hinv.eol_eq.mpr hq.1) hb
  · -- accept
    rename_i hop
    have hop' : p.op pc = .accept := hop
    obtain ⟨hm', hbest, hsel, himp⟩ := paConsider_spec p.k st v.1 v.2 st.pos hlen hinv.bestLen
    have hslot : ∀ sj, (paConsider p.k st v.1 v.2 st.pos).slot sj = st.slot sj := fun sj => paConsider_slot _ _ _ _ _ _
    have hcand : Cand p atoms input v.1 v.2 (chainPos input pos) := ⟨pos, pc, hReach, hop', rfl⟩
    have hflags := paConsider_flags p.k st v.1 v.2 st.pos
    refine ⟨?_, fun hm => ⟨hm', himp hm⟩⟩
    refine ⟨by simpa using hinv.slotsSize, hinv.siLt, by rw [hslot]; exact hinv.tableSize,
      by rw [paConsider_pos]; exact hinv.pos_eq, by rw [hflags.1]; exact hinv.bol_eq,
      by rw [hflags.2]; exact hinv.eol_eq, by simpa using hinv.queueLt,
      paConsider_bestCtr_length _ _ _ _ _ hlen hinv.bestLen, by rw [hslot]; exact hinv.lens,
      by rw [hslot]; exact hinv.sound, ?_, ?_, by rw [hslot]; exact hinv.rslot⟩
    · intro r hr hf _
      rw [hslot] at hf ⊢
      by_cases hrpc : r = pc
      · rw [hrpc]
        right
        refine ⟨fun q hq => ?_, fun _ => ⟨hm', ?_⟩⟩
        · simp only [EpsEdge, hop'] at hq
        · rw [← hpos]; exact hsel
      · rcases hinv.queued r hr hf (by simpa using hrpc) with hmem | hdone
        · left; simpa using hmem
        · right
          rw [hpos]
          exact Done_paConsider p input pos si st r v.1 v.2 _ hlen hinv.bestLen hdone
    · intro _
      rcases hbest with h | ⟨hm, h⟩
      · simp only [bestOf, Prod.mk.injEq] at h
        rw [h.1, h.2.1, h.2.2, hpos]
        exact hcand
      · simp only [bestOf, Prod.mk.injEq] at h
        rw [h.1, h.2.1, h.2.2]
        try exact hinv.bestCand hm
  · -- no epsilon edge and no accept
    rename_i h1 h2 h3 h4 h5
    refine ⟨hinv.close fun _ => Or.inr ⟨fun q hq => ?_, fun ha => ?_⟩, fun hm => ⟨hm, selLE_refl _⟩⟩
    · simp only [EpsEdge, Prog.op] at hq
      try (split at hq <;> rename_i heq <;>
        first | exact h1 heq | exact h2 heq | exact h3 heq | exact h4 heq | exact hq.elim)
    · exact (h5 ha).elim

/-! ## The drain loop -/

theorem handle_correct (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (pos si : Nat) (st : St) (pc : Nat)
    (hinv : CInvX p atoms input pos si st (some pc)) (hpc : pc < p.n) :
    CInvX p atoms input pos si (handle p si st pc) none ∧
    (st.matched = true → (handle p si st pc).matched = true ∧ selLE (bestOf (handle p si st pc)) (bestOf st)) := by
  unfold handle
  split
  · rename_i hstale
    refine ⟨hinv.close fun hf => ?_, fun hm => ⟨hm, selLE_refl _⟩⟩
    unfold Fresh at hf
    exact absurd hf (by simpa using hstale)
  · rename_i hfresh
    simp only [bne_iff_ne, ne_eq, Decidable.not_not] at hfresh
    exact handleOp_correct p atoms input hwf pos si st pc hinv hfresh hpc

theorem pop_correct (p : Prog) (atoms : Atoms) (input : Input) (pos si : Nat) (st : St) (pc : Nat) (rest : List Nat)
    (hinv : CInvX p atoms input pos si st none) (hq : st.queue = pc :: rest) :
    CInvX p atoms input pos si (st.popQueue rest) (some pc) :=
  ⟨hinv.slotsSize, hinv.siLt, hinv.tableSize, hinv.pos_eq, hinv.bol_eq, hinv.eol_eq,
   fun r hr => hinv.queueLt r (by rw [hq]; exact List.mem_cons_of_mem _ hr), hinv.bestLen, hinv.lens, hinv.sound,
   fun r hr hf hne => by
     rcases hinv.queued r hr hf (by simp) with hmem | hdone
     · rw [hq] at hmem
       rcases List.mem_cons.mp hmem with h | h
       · exact absurd (by rw [h]) hne
       · exact Or.inl h
     · exact Or.inr hdone,
   hinv.bestCand, hinv.rslot⟩

theorem compact_correct (p : Prog) (atoms : Atoms) (input : Input) (pos si : Nat) (st : St)
    (hinv : CInvX p atoms input pos si st none) : CInvX p atoms input pos si (compactQueue st) none :=
  ⟨hinv.slotsSize, hinv.siLt, hinv.tableSize, hinv.pos_eq, hinv.bol_eq, hinv.eol_eq,
   fun r hr => hinv.queueLt r ((compactQueue_mem st r).mp hr), hinv.bestLen, hinv.lens, hinv.sound,
   fun r hr hf hne => by
     rcases hinv.queued r hr hf hne with hmem | hdone
     · exact Or.inl ((compactQueue_mem st r).mpr hmem)
     · exact Or.inr hdone,
   hinv.bestCand, hinv.rslot⟩

theorem closureStep_correct (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (pos si : Nat) (st : St)
    (hinv : CInvX p atoms input pos si st none) :
    CInvX p atoms input pos si (closureStep p st si) none ∧
    (st.matched = true → (closureStep p st si).matched = true ∧ selLE (bestOf (closureStep p st si)) (bestOf st)) := by
  unfold closureStep
  have hc : CInvX p atoms input pos si (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st)
      none := by
    split
    · exact compact_correct p atoms input pos si st hinv
    · exact hinv
  have hb : bestOf (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = bestOf st ∧
      (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).matched = st.matched := by
    split <;> exact ⟨rfl, rfl⟩
  generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = st1 at hc hb
  unfold drain
  split
  · exact ⟨hc, fun hm => ⟨by rw [hb.2]; exact hm, by rw [hb.1]; exact selLE_refl _⟩⟩
  · rename_i pc rest hq
    have hpc : pc < p.n := hc.queueLt pc (by rw [hq]; simp)
    obtain ⟨h1, h2⟩ := handle_correct p atoms input hwf pos si (st1.popQueue rest) pc (pop_correct p atoms input pos si st1 pc rest hc hq) hpc
    refine ⟨h1, fun hm => ?_⟩
    have hm1 : (st1.popQueue rest).matched = true := by rw [St.popQueue]; simpa [hb.2] using hm
    obtain ⟨h3, h4⟩ := h2 hm1
    refine ⟨h3, ?_⟩
    have : bestOf (st1.popQueue rest) = bestOf st := by rw [← hb.1]; rfl
    rw [← this]; exact h4

theorem paClosure_correct (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (pos si : Nat) :
    ∀ (fuel : Nat) (st : St), CInvX p atoms input pos si st none →
      CInvX p atoms input pos si (paClosure p si st fuel) none ∧
      (st.matched = true → (paClosure p si st fuel).matched = true ∧
        selLE (bestOf (paClosure p si st fuel)) (bestOf st)) := by
  intro fuel
  induction fuel with
  | zero => intro st h; exact ⟨h, fun hm => ⟨hm, selLE_refl _⟩⟩
  | succ fuel ih =>
    intro st h
    simp only [paClosure]
    split
    · exact ⟨h, fun hm => ⟨hm, selLE_refl _⟩⟩
    · obtain ⟨h1, h2⟩ := closureStep_correct p atoms input hwf pos si st h
      obtain ⟨h3, h4⟩ := ih _ h1
      refine ⟨h3, fun hm => ?_⟩
      obtain ⟨h5, h6⟩ := h2 hm
      obtain ⟨h7, h8⟩ := h4 h5
      exact ⟨h7, selLE_trans _ _ _ h8 h6⟩

/-- With the queue drained, every fresh instruction has discharged its obligations. -/
theorem closed_done (p : Prog) (atoms : Atoms) (input : Input) (pos si : Nat) (st : St)
    (hinv : CInvX p atoms input pos si st none) (hempty : st.queue = []) :
    ∀ pc, pc < p.n → Fresh (st.slot si) pc → Done p input pos si st pc (payloadAt (st.slot si) pc) := by
  intro pc hpc hf
  rcases hinv.queued pc hpc hf (by simp) with hmem | hdone
  · rw [hempty] at hmem; simp at hmem
  · exact hdone

/-! ## From held entry threads to every thread of the boundary -/

/-- A step never moves backwards, and a consuming step below the end moves forward. -/
theorem stepPos_ge (input : Input) (pos : Nat) : pos ≤ stepPos input pos := by
  unfold stepPos; omega

theorem advance_ge (input : Input) (pos : Nat) : ∀ d, pos ≤ advance input pos d := by
  intro d
  induction d generalizing pos with
  | zero => exact Nat.le_refl _
  | succ d ih => exact Nat.le_trans (stepPos_ge input pos) (ih _)

theorem stepPos_gt (input : Input) (pos : Nat) (h : pos < input.bytes.size) : pos < stepPos input pos := by
  unfold stepPos sizeAt
  rw [if_neg (by simp; omega)]
  have := (decodeRuneAt_size input.bytes pos h).1
  omega

theorem advance_gt (input : Input) (pos : Nat) (d : Nat) (hd : 1 ≤ d) (h : pos < input.bytes.size) :
    pos < advance input pos d := by
  cases d with
  | zero => omega
  | succ d =>
    simp only [advance]
    exact Nat.lt_of_lt_of_le (stepPos_gt input pos h) (advance_ge input _ d)

theorem Step_i (p : Prog) (atoms : Atoms) (input : Input) (a b : Th) (h : Step p atoms input a b) : a.i ≤ b.i := by
  cases h with
  | eps q _ => exact Nat.le_refl _
  | consume delta _ => exact Nat.le_add_right _ _

theorem aheadList_end (input : Input) (pos fuel : Nat) (h : ¬ pos < input.bytes.size) :
    aheadList input pos (fuel + 1) = [] := by
  simp [aheadList, h]

theorem Consumes_pos_lt (p : Prog) (atoms : Atoms) (input : Input) (pos pc delta : Nat)
    (hlens : ∀ len ∈ atoms.lens pc, 2 ≤ len) (h : Consumes p atoms input pos pc delta) :
    chainPos input pos < input.bytes.size ∧ 1 ≤ delta := by
  obtain ⟨_, h⟩ := h
  rcases h with ⟨hd, hp, _⟩ | ⟨_, hd, hl, _⟩
  · exact ⟨hp, by omega⟩
  · have h2 := hlens delta hd
    refine ⟨?_, by omega⟩
    by_cases hge : chainPos input pos < input.bytes.size
    · exact hge
    · exfalso
      have : aheadAt input (chainPos input pos) = [] := by
        unfold aheadAt
        exact aheadList_end input _ 7 hge
      rw [this] at hl
      simp at hl
      omega

theorem Steps_i (p : Prog) (atoms : Atoms) (input : Input) (a b : Th) (h : Steps p atoms input a b) :
    a.i ≤ b.i := by
  induction h with
  | refl => exact Nat.le_refl _
  | tail b c _ s ih => exact Nat.le_trans ih (Step_i p atoms input b c s)

theorem advance_add (input : Input) (pos a b : Nat) :
    advance input pos (a + b) = advance input (advance input pos a) b := by
  induction a generalizing pos with
  | zero => rw [Nat.zero_add]; rfl
  | succ a ih =>
    rw [Nat.succ_add]
    simp only [advance]
    exact ih _

theorem chainPos_mono (input : Input) (i j : Nat) (h : i ≤ j) : chainPos input i ≤ chainPos input j := by
  unfold chainPos
  obtain ⟨d, rfl⟩ := Nat.exists_eq_add_of_le h
  rw [advance_add]
  exact advance_ge input _ d

/-- Threads carry a start at or before their position. -/
theorem Steps_start (p : Prog) (atoms : Atoms) (input : Input) (a b : Th) (h : Steps p atoms input a b) : b.s = a.s := by
  induction h with
  | refl => rfl
  | tail b c _ s ih =>
    cases s with
    | eps q _ => exact ih
    | consume delta _ => exact ih

theorem Reach_start_le (p : Prog) (atoms : Atoms) (input : Input) (T : Th) (h : Reach p atoms input T) :
    T.s ≤ chainPos input T.i := by
  obtain ⟨q, _, hs⟩ := h
  have h1 := Steps_start p atoms input _ _ hs
  have h2 := Steps_i p atoms input _ _ hs
  simp only [spawnTh] at h1 h2
  rw [h1]
  exact chainPos_mono input _ _ h2

/-- Well-formed multi-character probes: every probed length is between two and the ring size. -/
def Atoms.wf2 (p : Prog) (atoms : Atoms) : Prop := ∀ pc len, len ∈ atoms.lens pc → 2 ≤ len ∧ len < p.ring

/-- A thread's payload, as a pair. -/
def Th.payload (T : Th) : Payload := (T.s, T.c)

/-- A thread is covered by the state when the slot holds a payload at least as good, or it is dominated. -/
def Covered (k : Nat) (st : St) (si : Nat) (T : Th) : Prop :=
  Holds k (st.slot si) T.pc T.payload ∨ Dom k st T.payload

/-! Persistence of coverage through a closure. -/

theorem handleOp_persist (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (pos si : Nat) (st : St) (pc : Nat)
    (hinv : CInvX p atoms input pos si st (some pc)) (hfresh : Fresh (st.slot si) pc) (hpc : pc < p.n) (r : Nat)
    (w : Payload) (hr : r < p.n) :
    let v := payloadAt (st.slot si) pc
    (Holds p.k (st.slot si) r w → Holds p.k ((handleOp p si st pc v.1 v.2).slot si) r w) ∧
    (Dom p.k st w → Dom p.k (handleOp p si st pc v.1 v.2) w) := by
  intro v
  have hReach : Reach p atoms input ⟨pos, pc, v.1, v.2⟩ := hinv.sound pc hpc hfresh
  have hlen : v.2.length = p.k := hinv.lens pc hpc
  have hedge_split : p.op pc = .split → EpsEdge p input pos pc (p.ins.getD pc default).next ∧
      EpsEdge p input pos pc (p.ins.getD pc default).alt := by
    intro h; simp [EpsEdge, h, Prog.next, Prog.alt]
  have hedge_jmp : p.op pc = .jmp → EpsEdge p input pos pc (p.ins.getD pc default).next := by
    intro h; simp [EpsEdge, h, Prog.next]
  have hedge_bol : p.op pc = .bol → bolRef input pos →
      EpsEdge p input pos pc (p.ins.getD pc default).next := by
    intro h hb; simp [EpsEdge, h, hb, Prog.next]
  have hedge_eol : p.op pc = .eol → eolRef input pos →
      EpsEdge p input pos pc (p.ins.getD pc default).next := by
    intro h he; simp [EpsEdge, h, he, Prog.next]
  unfold handleOp
  split
  · rename_i hop
    obtain ⟨he1, he2⟩ := hedge_split hop
    obtain ⟨hi1, _, _, _, _, hholds1, hdom1, _, _⟩ := relax_transition p atoms input hwf pos si st pc _ v hinv hpc hReach hlen he1
    obtain ⟨_, _, _, _, _, hholds2, hdom2, _, _⟩ := relax_transition p atoms input hwf pos si _ pc _ v hi1 hpc hReach hlen he2
    exact ⟨fun h => hholds2 r w hr (hholds1 r w hr h), fun h => hdom2 w (hdom1 w h)⟩
  · rename_i hop
    obtain ⟨_, _, _, _, _, hholds1, hdom1, _, _⟩ :=
      relax_transition p atoms input hwf pos si st pc _ v hinv hpc hReach hlen (hedge_jmp hop)
    exact ⟨fun h => hholds1 r w hr h, fun h => hdom1 w h⟩
  · rename_i hop
    by_cases hb : st.bol = true
    · rw [if_pos hb]
      obtain ⟨_, _, _, _, _, hholds1, hdom1, _, _⟩ :=
        relax_transition p atoms input hwf pos si st pc _ v hinv hpc hReach hlen (hedge_bol hop (hinv.bol_eq.mp hb))
      exact ⟨fun h => hholds1 r w hr h, fun h => hdom1 w h⟩
    · rw [if_neg hb]; exact ⟨id, id⟩
  · rename_i hop
    by_cases hb : st.eol = true
    · rw [if_pos hb]
      obtain ⟨_, _, _, _, _, hholds1, hdom1, _, _⟩ :=
        relax_transition p atoms input hwf pos si st pc _ v hinv hpc hReach hlen (hedge_eol hop (hinv.eol_eq.mp hb))
      exact ⟨fun h => hholds1 r w hr h, fun h => hdom1 w h⟩
    · rw [if_neg hb]; exact ⟨id, id⟩
  · exact ⟨fun h => Holds_paConsider _ _ _ _ _ _ _ _ h, fun h => Dom_paConsider p.k st v.1 v.2 st.pos w h hlen hinv.bestLen⟩
  · exact ⟨id, id⟩

theorem closureStep_persist (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (pos si : Nat) (st : St)
    (hinv : CInvX p atoms input pos si st none) (r : Nat) (w : Payload) (hr : r < p.n) :
    (Holds p.k (st.slot si) r w → Holds p.k ((closureStep p st si).slot si) r w) ∧
    (Dom p.k st w → Dom p.k (closureStep p st si) w) := by
  unfold closureStep
  have hc : CInvX p atoms input pos si (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st)
      none := by
    split
    · exact compact_correct p atoms input pos si st hinv
    · exact hinv
  have hsame : (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).slot si = st.slot si ∧
      Dom p.k (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) w = Dom p.k st w := by
    split <;> exact ⟨rfl, rfl⟩
  generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = st1 at hc hsame
  rw [← hsame.1, ← hsame.2]
  unfold drain
  split
  · exact ⟨id, id⟩
  · rename_i pc rest hq
    have hpc : pc < p.n := hc.queueLt pc (by rw [hq]; simp)
    have hinv2 := pop_correct p atoms input pos si st1 pc rest hc hq
    unfold handle
    split
    · exact ⟨id, id⟩
    · rename_i hfresh
      simp only [bne_iff_ne, ne_eq, Decidable.not_not] at hfresh
      have := handleOp_persist p atoms input hwf pos si (st1.popQueue rest) pc hinv2 hfresh hpc r w hr
      exact ⟨fun h => this.1 h, fun h => this.2 (by unfold Dom at h ⊢; exact h)⟩

theorem paClosure_persist (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (pos si : Nat) (r : Nat)
    (w : Payload) (hr : r < p.n) :
    ∀ (fuel : Nat) (st : St), CInvX p atoms input pos si st none →
      (Holds p.k (st.slot si) r w → Holds p.k ((paClosure p si st fuel).slot si) r w) ∧
      (Dom p.k st w → Dom p.k (paClosure p si st fuel) w) := by
  intro fuel
  induction fuel with
  | zero => intro st _; exact ⟨id, id⟩
  | succ fuel ih =>
    intro st h
    simp only [paClosure]
    split
    · exact ⟨id, id⟩
    · obtain ⟨h1, _⟩ := closureStep_correct p atoms input hwf pos si st h
      have h2 := closureStep_persist p atoms input hwf pos si st h r w hr
      have h3 := ih _ h1
      exact ⟨fun h => h3.1 (h2.1 h), fun h => h3.2 (h2.2 h)⟩

/-- After the closure, every thread at the boundary that descends by epsilon steps from a covered thread is covered. -/
theorem closed_holds (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (hawf : atoms.wf2 p) (pos si : Nat)
    (st : St) (hinv : CInvX p atoms input pos si st none) (hempty : st.queue = []) :
    ∀ (T0 T : Th), Steps p atoms input T0 T → T0.i = pos → T.i = pos → T0.pc < p.n → T0.c.length = p.k →
      Covered p.k st si T0 → Covered p.k st si T := by
  intro T0 T h
  induction h with
  | refl => intro _ _ _ _ hc; exact hc
  | tail b c hab s ih =>
    intro h0 hc hpc0 hlen0 hcov
    have hb : b.i = pos := by
      have h1 := Steps_i p atoms input _ _ hab
      have h2 := Step_i p atoms input _ _ s
      omega
    have hbcov := ih h0 hb hpc0 hlen0 hcov
    have hbpc : b.pc < p.n := Steps_pc p atoms input hwf _ _ hab hpc0
    have hblen : b.c.length = p.k := by rw [Steps_len p atoms input _ _ hab]; exact hlen0
    cases s with
    | eps q he =>
      rw [hb] at he
      rcases hbcov with hh | hd
      · -- The stored payload is at or below the thread's, and its instruction is done.
        have hdone := closed_done p atoms input pos si st hinv hempty b.pc hbpc hh.1
        rcases hdone.1 q he with h1 | h1
        · left
          simp only [Th.payload] at hh ⊢
          exact ⟨h1.1, ple_trans p.k _ _ _ h1.2 hh.2⟩
        · right
          exact Dom_of_ple p.k st _ _ h1 hh.2
      · right; exact hd
    | consume delta hcons =>
      exfalso
      have hlt := Consumes_pos_lt p atoms input b.i b.pc delta (fun len hl => (hawf b.pc len hl).1) hcons
      have hc' : b.i + delta = pos := hc
      omega

/-! ## Positions of consecutive boundaries -/

theorem chainPos_succ (input : Input) (i : Nat) : chainPos input (i + 1) = stepPos input (chainPos input i) := by
  unfold chainPos
  rw [advance_add]
  rfl

theorem stepPos_le_size (input : Input) (pos : Nat) (h : pos ≤ input.bytes.size) : stepPos input pos ≤ input.bytes.size := by
  unfold stepPos sizeAt
  by_cases he : pos = input.bytes.size
  · simp [he]
  · rw [if_neg (by simpa using he)]
    exact (decodeRuneAt_size input.bytes pos (by omega)).2

theorem chainPos_le_size (input : Input) (i : Nat) : chainPos input i ≤ input.bytes.size := by
  induction i with
  | zero => simp [chainPos, advance]
  | succ i ih => rw [chainPos_succ]; exact stepPos_le_size input _ ih

theorem chainPos_zero (input : Input) : chainPos input 0 = 0 := rfl

theorem UInt8.toNat_lt_of_lt {a b : UInt8} (h : a < b) : a.toNat < b.toNat := UInt8.lt_iff_toNat_lt.mp h
theorem UInt8.toNat_le_of_not_lt {a b : UInt8} (h : ¬ a < b) : b.toNat ≤ a.toNat :=
  UInt8.le_iff_toNat_le.mp (UInt8.not_lt.mp h)

set_option maxHeartbeats 1000000 in
/-- A character below `0x80` is one byte, and that byte. -/
theorem decodeOne_small (bs : ByteArray) (i c size : Nat) (h : Ere.decodeOne bs i = some (c, size)) (hc : c < 0x80) :
    size = 1 ∧ bs.get! i = UInt8.ofNat c := by
  unfold Ere.decodeOne at h
  split at h
  · rename_i h0
    simp only at h
    have hor : ∀ a b : Nat, a ≤ a ||| b := fun a b => Nat.left_le_or
    have hor' : ∀ a b : Nat, b ≤ a ||| b := fun a b => Nat.right_le_or
    have hsh : ∀ a s : Nat, a <<< s = a * 2 ^ s := Nat.shiftLeft_eq
    split at h
    · rename_i hlt
      simp only [Option.some.injEq, Prod.mk.injEq] at h
      obtain ⟨hc', hs⟩ := h
      refine ⟨hs.symm, ?_⟩
      rw [← hc']
      simp [ByteArray.get!, h0]
      rfl
    · rename_i hge0
      split at h
      · simp at h
      · rename_i hge1
        split at h
        · -- two bytes
          rename_i hlt2
          split at h
          · rename_i c1 heq
            split at h
            · simp only [Option.some.injEq, Prod.mk.injEq] at h
              exfalso
              rw [← h.1] at hc
              have h4 := UInt8.toNat_le_of_not_lt hge1
              have h5 := UInt8.toNat_lt_of_lt hlt2
              simp at h4 h5
              have key : 128 ≤ (bs[i].toNat &&& 0x1f) <<< 6 := by
                rw [hsh, Nat.and_two_pow_sub_one_eq_mod _ 5]
                have : 2 ≤ bs[i].toNat % 32 := by omega
                omega
              have := Nat.le_trans key (hor ((bs[i].toNat &&& 0x1f) <<< 6) (c1.toNat &&& 0x3f))
              omega
            · simp at h
          · simp at h
        · rename_i hge2
          split at h
          · -- three bytes
            rename_i hlt3
            split at h
            · rename_i c1 c2 heq1 heq2
              split at h
              · simp at h
              · rename_i hcont
                split at h
                · simp at h
                · rename_i hnot1
                  split at h
                  · simp at h
                  · simp only [Option.some.injEq, Prod.mk.injEq] at h
                    exfalso
                    rw [← h.1] at hc
                    have h4 := UInt8.toNat_le_of_not_lt hge2
                    have h5 := UInt8.toNat_lt_of_lt hlt3
                    simp at h4 h5
                    have hE1 := hor ((bs[i].toNat &&& 0x0f) <<< 12 ||| (c1.toNat &&& 0x3f) <<< 6) (c2.toNat &&& 0x3f)
                    have hA := Nat.le_trans (hor ((bs[i].toNat &&& 0x0f) <<< 12) ((c1.toNat &&& 0x3f) <<< 6)) hE1
                    have hB := Nat.le_trans (hor' ((bs[i].toNat &&& 0x0f) <<< 12) ((c1.toNat &&& 0x3f) <<< 6)) hE1
                    have hA' : (bs[i].toNat &&& 0x0f) <<< 12 = (bs[i].toNat % 16) * 4096 := by
                      rw [hsh, Nat.and_two_pow_sub_one_eq_mod _ 4]
                    have hB' : (c1.toNat &&& 0x3f) <<< 6 = (c1.toNat % 64) * 64 := by
                      rw [hsh, Nat.and_two_pow_sub_one_eq_mod _ 6]
                    by_cases he : bs[i].toNat = 0xe0
                    · have hb : bs[i] = 0xe0 := by
                        apply UInt8.toNat.inj; simpa using he
                      simp only [hb, beq_self_eq_true, Bool.true_and, Bool.not_eq_true'] at hnot1
                      have hc1 := UInt8.toNat_le_of_not_lt (by simpa using hnot1 : ¬ c1 < 0xa0)
                      simp at hc1
                      have hcont1 : Ere.cont c1 = true := by
                        simp only [Bool.or_eq_true, Bool.not_eq_true', not_or, Bool.not_eq_false] at hcont
                        exact hcont.1
                      have hc1u : c1.toNat ≤ 191 := by
                        simp only [Ere.cont, Bool.and_eq_true, decide_eq_true_eq] at hcont1
                        have := UInt8.le_iff_toNat_le.mp hcont1.2
                        simpa using this
                      have : 32 ≤ c1.toNat % 64 := by omega
                      omega
                    · have : 1 ≤ bs[i].toNat % 16 := by omega
                      omega
            · simp at h
          · rename_i hge3
            split at h
            · -- four bytes
              rename_i hlt4
              split at h
              · rename_i c1 c2 c3 heq1 heq2 heq3
                split at h
                · simp at h
                · rename_i hcont
                  split at h
                  · simp at h
                  · rename_i hnot1
                    split at h
                    · simp at h
                    · simp only [Option.some.injEq, Prod.mk.injEq] at h
                      exfalso
                      rw [← h.1] at hc
                      have h4 := UInt8.toNat_le_of_not_lt hge3
                      have h5 := UInt8.toNat_lt_of_lt hlt4
                      simp at h4 h5
                      have hE1 := hor ((bs[i].toNat &&& 0x07) <<< 18 ||| (c1.toNat &&& 0x3f) <<< 12 |||
                        (c2.toNat &&& 0x3f) <<< 6) (c3.toNat &&& 0x3f)
                      have hE2 := Nat.le_trans (hor ((bs[i].toNat &&& 0x07) <<< 18 ||| (c1.toNat &&& 0x3f) <<< 12)
                        ((c2.toNat &&& 0x3f) <<< 6)) hE1
                      have hA := Nat.le_trans (hor ((bs[i].toNat &&& 0x07) <<< 18) ((c1.toNat &&& 0x3f) <<< 12)) hE2
                      have hB := Nat.le_trans (hor' ((bs[i].toNat &&& 0x07) <<< 18) ((c1.toNat &&& 0x3f) <<< 12)) hE2
                      have hA' : (bs[i].toNat &&& 0x07) <<< 18 = (bs[i].toNat % 8) * 262144 := by
                        rw [hsh, Nat.and_two_pow_sub_one_eq_mod _ 3]
                      have hB' : (c1.toNat &&& 0x3f) <<< 12 = (c1.toNat % 64) * 4096 := by
                        rw [hsh, Nat.and_two_pow_sub_one_eq_mod _ 6]
                      by_cases he : bs[i].toNat = 0xf0
                      · have hb : bs[i] = 0xf0 := by
                          apply UInt8.toNat.inj; simpa using he
                        simp only [hb, beq_self_eq_true, Bool.true_and, Bool.not_eq_true'] at hnot1
                        have hc1 := UInt8.toNat_le_of_not_lt (by simpa using hnot1 : ¬ c1 < 0x90)
                        simp at hc1
                        have hcont1 : Ere.cont c1 = true := by
                          simp only [Bool.or_eq_true, Bool.not_eq_true', not_or, Bool.not_eq_false] at hcont
                          exact hcont.1.1
                        have hc1u : c1.toNat ≤ 191 := by
                          simp only [Ere.cont, Bool.and_eq_true, decide_eq_true_eq] at hcont1
                          have := UInt8.le_iff_toNat_le.mp hcont1.2
                          simpa using this
                        have : 16 ≤ c1.toNat % 64 := by omega
                        omega
                      · have : 1 ≤ bs[i].toNat % 8 := by omega
                        omega
              · simp at h
            · simp at h
  · simp at h

/-! ## Productivity backwards and domination forwards along steps -/

theorem Steps_trans (p : Prog) (atoms : Atoms) (input : Input) (a b c : Th) (h1 : Steps p atoms input a b)
    (h2 : Steps p atoms input b c) : Steps p atoms input a c := by
  induction h2 with
  | refl => exact h1
  | tail b' c' _ s' ih => exact Steps.tail _ _ _ ih s'

theorem Prod_of_step (p : Prog) (atoms : Atoms) (input : Input) (a b : Th) (s : Step p atoms input a b)
    (h : Prod p atoms input b) : Prod p atoms input a := by
  obtain ⟨T', hs, ha⟩ := h
  exact ⟨T', Steps_trans p atoms input a b T' (Steps.tail a a b (Steps.refl a) s) hs, ha⟩

theorem Prod_of_steps (p : Prog) (atoms : Atoms) (input : Input) (a b : Th) (hs : Steps p atoms input a b)
    (h : Prod p atoms input b) : Prod p atoms input a := by
  induction hs with
  | refl => exact h
  | tail b' c' _ s' ih => exact ih (Prod_of_step p atoms input _ _ s' h)

theorem Dom_step (p : Prog) (atoms : Atoms) (input : Input) (st : St) (a b : Th) (s : Step p atoms input a b)
    (h : Dom p.k st a.payload) : Dom p.k st b.payload := by
  cases s with
  | eps q _ => exact h
  | consume delta _ => exact Dom_bumpIf p a.pc delta st a.payload h

theorem Dom_steps (p : Prog) (atoms : Atoms) (input : Input) (st : St) (a b : Th) (hs : Steps p atoms input a b)
    (h : Dom p.k st a.payload) : Dom p.k st b.payload := by
  induction hs with
  | refl => exact h
  | tail b' c' _ s' ih => exact Dom_step p atoms input st _ _ s' ih

/-- Steps that end past a threshold index cross it with one consuming step from a thread at or below it. -/
theorem Steps_cross (p : Prog) (atoms : Atoms) (input : Input) (lim : Nat) (a b : Th)
    (hs : Steps p atoms input a b) (ha : a.i ≤ lim) (hb : lim < b.i) :
    ∃ T0 delta, Steps p atoms input a T0 ∧ T0.i ≤ lim ∧ Consumes p atoms input T0.i T0.pc delta ∧
      lim < T0.i + delta ∧
      Steps p atoms input ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩ b := by
  induction hs with
  | refl => omega
  | tail b' c' hab' s' ih =>
    by_cases hb' : lim < b'.i
    · obtain ⟨T0, delta, h1, h2, h3, h4, h5⟩ := ih hb'
      exact ⟨T0, delta, h1, h2, h3, h4, Steps.tail _ _ _ h5 s'⟩
    · have hle : b'.i ≤ lim := by omega
      cases s' with
      | eps q _ => exact absurd hb (by simp; omega)
      | consume delta hc =>
        exact ⟨b', delta, hab', hle, hc, by simpa using hb, Steps.refl _⟩

/-! ## The lookahead and the boundaries it spans -/

theorem aheadList_length_le (input : Input) (pos fuel : Nat) : (aheadList input pos fuel).length ≤ fuel := by
  induction fuel generalizing pos with
  | zero => simp [aheadList]
  | succ fuel ih =>
    simp only [aheadList]
    split
    · simp only [List.length_cons]; have := ih (stepPos input pos); omega
    · simp

/-- `d` characters of lookahead from a boundary mean `d - 1` boundaries after it sit before the end. -/
theorem aheadList_chars (input : Input) :
    ∀ (fuel pos d : Nat), d ≤ (aheadList input pos fuel).length → 1 ≤ d → advance input pos (d - 1) < input.bytes.size := by
  intro fuel
  induction fuel with
  | zero => intro pos d h hd; simp [aheadList] at h; omega
  | succ fuel ih =>
    intro pos d h hd
    simp only [aheadList] at h
    split at h
    · rename_i hlt
      cases d with
      | zero => omega
      | succ d =>
        cases d with
        | zero => exact hlt
        | succ d =>
          simp only [List.length_cons] at h
          have := ih (stepPos input pos) (d + 1) (by omega) (by omega)
          simp only [Nat.add_sub_cancel] at this ⊢
          simp only [advance]
          exact this
    · simp at h; omega

/-- A consuming step lands on a boundary of the subject. -/
theorem Consumes_valid (p : Prog) (atoms : Atoms) (input : Input) (i pc delta : Nat)
    (hlens : ∀ len ∈ atoms.lens pc, 2 ≤ len) (h : Consumes p atoms input i pc delta) :
    ValidIdx input (i + delta) := by
  obtain ⟨_, h⟩ := h
  rcases h with ⟨hd, hp, _⟩ | ⟨_, hd, hl, _⟩
  · subst hd; right; simpa using hp
  · have h2 := hlens delta hd
    right
    have := aheadList_chars input maxElemAhead (chainPos input i) delta hl (by omega)
    unfold chainPos
    rw [show i + delta - 1 = i + (delta - 1) by omega, advance_add]
    exact this

theorem Steps_valid (p : Prog) (atoms : Atoms) (input : Input) (hawf : atoms.wf2 p) (a b : Th)
    (hs : Steps p atoms input a b) (ha : ValidIdx input a.i) : ValidIdx input b.i := by
  induction hs with
  | refl => exact ha
  | tail b' c' _ s' ih =>
    cases s' with
    | eps q _ => exact ih
    | consume delta hc => exact Consumes_valid p atoms input b'.i b'.pc delta (fun len hl => (hawf b'.pc len hl).1) hc

theorem Reach_valid (p : Prog) (atoms : Atoms) (input : Input) (hawf : atoms.wf2 p) (T : Th)
    (h : Reach p atoms input T) : ValidIdx input T.i := by
  obtain ⟨q, hv, hs⟩ := h
  exact Steps_valid p atoms input hawf _ _ hs hv

theorem valid_lt (input : Input) (i : Nat) (hv : ValidIdx input i) (hi : 1 ≤ i) : chainPos input (i - 1) < input.bytes.size := by
  rcases hv with h | h
  · omega
  · exact h

/-! ## Arrivals, slots and the run invariant -/

/-- A thread delivered by a consuming step from a thread at a boundary below `lim`. -/
def Arrived (p : Prog) (atoms : Atoms) (input : Input) (lim : Nat) (T : Th) : Prop :=
  ∃ T0 delta, Reach p atoms input T0 ∧ T0.i < lim ∧ Consumes p atoms input T0.i T0.pc delta ∧
    T = ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩

theorem Arrived_reach (p : Prog) (atoms : Atoms) (input : Input) (lim : Nat) (T : Th)
    (h : Arrived p atoms input lim T) : Reach p atoms input T := by
  obtain ⟨T0, delta, ⟨q, hv, hs⟩, _, hc, rfl⟩ := h
  exact ⟨q, hv, Steps.tail _ _ _ hs (Step.consume T0 delta hc)⟩

theorem Arrived_mono (p : Prog) (atoms : Atoms) (input : Input) (lim lim' : Nat) (T : Th) (h : lim ≤ lim')
    (ha : Arrived p atoms input lim T) : Arrived p atoms input lim' T := by
  obtain ⟨T0, delta, h1, h2, h3, h4⟩ := ha
  exact ⟨T0, delta, h1, by omega, h3, h4⟩

/-- The run invariant at the start of the boundary with index `idx`. -/
structure RunInv (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int) (idx : Nat) : Prop where
  slotsSize : st.slots.size = p.ring
  posEq : st.pos = chainPos input idx
  valid : ValidIdx input idx
  prevOK : (prev = 10) ↔ (0 < idx ∧ curAt input (chainPos input (idx - 1)) = 10)
  slots : ∀ d, d < p.ring → RSlotQ p (st.slot ((st.ci + d) % p.ring)) (paGen (st.ci + d))
  sound : ∀ d, d < p.ring → (st.slot ((st.ci + d) % p.ring)).gen = paGen (st.ci + d) →
    ∀ pc, pc < p.n → Fresh (st.slot ((st.ci + d) % p.ring)) pc →
      Arrived p atoms input idx (thOf (idx + d) (st.slot ((st.ci + d) % p.ring)) pc)
  complete : ∀ d, d < p.ring → ∀ T, Arrived p atoms input idx T → T.i = idx + d → Prod p atoms input T →
    ((st.slot ((st.ci + d) % p.ring)).gen = paGen (st.ci + d) ∧
      Holds p.k (st.slot ((st.ci + d) % p.ring)) T.pc T.payload) ∨ Dom p.k st T.payload
  bestLen : st.bestCtr.length = p.k
  bestCand : st.matched = true → Cand p atoms input st.so st.bestCtr st.eo ∧ st.eo < st.pos
  seen : ∀ s c e, Cand p atoms input s c e → e < st.pos → st.matched = true ∧ selLE (bestOf st) (s, c, e)

/-! ## The flags of a boundary agree with the reference anchors -/

theorem curAt_eq (input : Input) (st : St) (prev : Int) : (st.setFlags input prev).cur = curAt input st.pos := rfl

/-- Only the first boundary of the subject sits at position zero. -/
theorem chainPos_eq_zero (input : Input) (idx : Nat) (hv : ValidIdx input idx) (h : chainPos input idx = 0) : idx = 0 := by
  cases idx with
  | zero => rfl
  | succ i =>
    exfalso
    have hlt := valid_lt input (i + 1) hv (by omega)
    simp only [Nat.add_sub_cancel] at hlt
    rw [chainPos_succ] at h
    have := stepPos_gt input _ hlt
    omega

theorem bol_eq_of_prevOK (input : Input) (idx : Nat) (prev : Int) (hv : ValidIdx input idx)
    (hprev : (prev = 10) ↔ (0 < idx ∧ curAt input (chainPos input (idx - 1)) = 10)) :
    bolAt input (chainPos input idx) prev = true ↔ bolRef input idx := by
  unfold bolAt bolRef
  simp only [Bool.or_eq_true, Bool.and_eq_true, beq_iff_eq, Bool.not_eq_true']
  constructor
  · rintro (⟨h1, h2⟩ | ⟨h1, h2⟩)
    · left; exact ⟨chainPos_eq_zero input idx hv h1, h2⟩
    · right; exact ⟨h1, hprev.mp h2⟩
  · rintro (⟨h1, h2⟩ | ⟨h1, h2, h3⟩)
    · left; subst h1; exact ⟨rfl, h2⟩
    · right; exact ⟨h1, hprev.mpr ⟨h2, h3⟩⟩

/-- Distinct boundaries of the subject sit at distinct positions. -/
theorem chainPos_inj (input : Input) (i j : Nat) (hi : ValidIdx input i) (hj : ValidIdx input j)
    (h : chainPos input i = chainPos input j) : i = j := by
  rcases Nat.lt_trichotomy i j with hlt | heq | hgt
  · exfalso
    have h1 := valid_lt input j hj (by omega)
    have h2 : chainPos input i ≤ chainPos input (j - 1) := chainPos_mono input i (j - 1) (by omega)
    have h3 : chainPos input j = stepPos input (chainPos input (j - 1)) := by
      rw [show j = (j - 1) + 1 by omega, chainPos_succ]; simp
    have h4 := stepPos_gt input _ h1
    omega
  · exact heq
  · exfalso
    have h1 := valid_lt input i hi (by omega)
    have h2 : chainPos input j ≤ chainPos input (i - 1) := chainPos_mono input j (i - 1) (by omega)
    have h3 : chainPos input i = stepPos input (chainPos input (i - 1)) := by
      rw [show i = (i - 1) + 1 by omega, chainPos_succ]; simp
    have h4 := stepPos_gt input _ h1
    omega

/-- A payload at or below a candidate's puts the candidate at or behind it in the selection order. -/
theorem selLE_of_ple (k : Nat) (a b : Payload) (e : Nat) (ha : a.2.length = k) (hb : b.2.length = k)
    (h : ple k a b) : selLE (a.1, a.2, e) (b.1, b.2, e) := by
  rcases h with h | h
  · simp only [plt, Bool.or_eq_true, Bool.and_eq_true, decide_eq_true_eq, beq_iff_eq, bne_iff_ne, ne_eq] at h
    simp only [selLE]
    rcases h with h | ⟨⟨h1, _⟩, h2⟩
    · left; exact h
    · right; exact ⟨h1, Or.inl h2⟩
  · simp only [selLE]
    right
    refine ⟨h.1, ?_⟩
    rcases h.2 with hk | hc
    · right
      subst hk
      have h1 := List.eq_nil_of_length_eq_zero ha
      have h2 := List.eq_nil_of_length_eq_zero hb
      exact ⟨h1.trans h2.symm, Nat.le_refl _⟩
    · right; exact ⟨hc, Nat.le_refl _⟩

/-- A dominated candidate is at or behind the best, whatever its end. -/
theorem selLE_of_Dom (k : Nat) (st : St) (s : Nat) (c : List Nat) (e : Nat) (h : Dom k st (s, c)) :
    selLE (bestOf st) (s, c, e) := by
  rw [Dom_iff] at h
  obtain ⟨_, h⟩ := h
  simp only [bestOf, selLE]
  rcases h with h | ⟨h1, _, h3⟩
  · left; exact h
  · right; exact ⟨h1.symm, Or.inl h3⟩

/-! ## Opening a boundary -/

/-- The current slot after the filter: its generation is the boundary's, and its entries are the live ones. -/
theorem filterSlot_current (st : St) (si g : Nat) (hsi : si < st.slots.size) :
    (st.filterSlot si g).slot si = { st.slot si with gen := g, active := liveAt st si g } := by
  simp only [St.filterSlot, St.slot, St.setSlot]
  simp [Array.getElem?_setIfInBounds, hsi]

theorem filterSlot_other (st : St) (si g sj : Nat) (h : si ≠ sj) : (st.filterSlot si g).slot sj = st.slot sj := by
  simp only [St.filterSlot, St.slot, St.setSlot]
  simp [Array.getElem?_setIfInBounds, h]

theorem liveAt_iff (st : St) (si g pc : Nat) : pc ∈ liveAt st si g ↔ pc ∈ (st.slot si).active ∧ ((st.slot si).entry pc).stamp = g := by
  unfold liveAt
  rw [List.mem_filter]
  simp

/-- A slot invariant survives the filter, and afterwards every fresh entry is live. -/
theorem RSlot_filter (p : Prog) (sl : Slot) (g : Nat) (h : RSlotQ p sl g) :
    RSlot p { sl with gen := g, active := sl.active.filter fun pc => (sl.entry pc).stamp == g } := by
  have hentry : ∀ pc, ({ sl with gen := g, active := sl.active.filter fun pc => (sl.entry pc).stamp == g } : Slot).entry pc =
      sl.entry pc := fun pc => rfl
  have hg := h.genLe
  refine ⟨h.size, fun pc hpc => by rw [hentry]; exact h.lens pc hpc,
    fun pc hpc => by rw [hentry]; exact Nat.le_trans (h.stampLe pc hpc) hg, ?_, ?_⟩
  · intro pc hpc
    rw [List.mem_filter] at hpc
    have hstamp : (sl.entry pc).stamp = g := by simpa using hpc.2
    by_cases hge : sl.gen = g
    · refine ⟨((h.cur hge).activeLt pc hpc.1).1, ?_⟩
      unfold Fresh; rw [hentry]; exact hstamp
    · exfalso
      by_cases hn : pc < p.n
      · have := h.stampLe pc hn; omega
      · have hnone : sl.table[pc]? = none := Array.getElem?_eq_none (by rw [h.size]; omega)
        simp only [Slot.entry_eq, hnone, Option.getD_none] at hstamp
        have : (default : Entry).stamp = 0 := rfl
        omega
  · intro pc hpc hf
    unfold Fresh at hf
    rw [hentry] at hf
    simp only at hf
    rw [List.mem_filter]
    have := h.stampLe pc hpc
    have hge : sl.gen = g := by omega
    have hf' : (sl.entry pc).stamp = sl.gen := by omega
    exact ⟨(h.cur hge).freshActive pc hpc hf', by simpa using hf⟩

/-- Opening the boundary with index `idx`: the current slot carries the boundary's generation and its live entries. -/
theorem open_boundary (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int) (idx : Nat)
    (hinv : RunInv p atoms input st prev idx) (hring : 2 ≤ p.ring) :
    let si := st.ci % p.ring
    let g := paGen st.ci
    let st1 := st.bumpBoundaries.filterSlot si g
    (st1.slot si).gen = g ∧ RSlot p (st1.slot si) ∧ (st1.slot si).active = liveAt st si g ∧
    (∀ pc, (st1.slot si).entry pc = (st.slot si).entry pc) ∧
    (∀ pc, pc < p.n → Fresh (st1.slot si) pc → Reach p atoms input (thOf idx (st1.slot si) pc)) ∧
    (∀ T, Arrived p atoms input idx T → T.i = idx → Prod p atoms input T →
      Holds p.k (st1.slot si) T.pc T.payload ∨ Dom p.k st T.payload) ∧
    (∀ sj, sj ≠ si → st1.slot sj = st.slot sj) ∧
    st1.pos = st.pos ∧ st1.ci = st.ci ∧ bestOf st1 = bestOf st ∧ st1.matched = st.matched ∧
    st1.bestCtr = st.bestCtr ∧ st1.slots.size = st.slots.size ∧ st1.queue = st.queue := by
  intro si g st1
  have hsi : si < st.slots.size := by rw [hinv.slotsSize]; exact Nat.mod_lt _ (by omega)
  have hcur : st1.slot si = { st.slot si with gen := g, active := liveAt st si g } := by
    simp only [st1]
    have := filterSlot_current st.bumpBoundaries si g (by simpa using hsi)
    simpa [St.bumpBoundaries, liveAt, St.slot] using this
  have hentry : ∀ pc, (st1.slot si).entry pc = (st.slot si).entry pc := by
    intro pc; rw [hcur]; rfl
  have h0 : (st.ci + 0) % p.ring = si := by simp [si]
  have hrs := hinv.slots 0 (by omega)
  rw [h0, Nat.add_zero] at hrs
  have hgenLe := hrs.genLe
  refine ⟨by rw [hcur], ?_, by rw [hcur], hentry, ?_, ?_, ?_, rfl, rfl, rfl, rfl, rfl, by simp [st1], rfl⟩
  · rw [hcur]
    exact RSlot_filter p (st.slot si) g hrs
  · intro pc hpc hf
    unfold Fresh at hf
    rw [hentry, hcur] at hf
    simp only at hf
    have hle := hrs.stampLe pc hpc
    have hgen : (st.slot si).gen = paGen (st.ci + 0) := by simp only [Nat.add_zero]; omega
    have := Arrived_reach p atoms input idx _
      (hinv.sound 0 (by omega) (by rw [h0]; exact hgen) pc hpc (by rw [h0]; unfold Fresh; omega))
    rw [h0] at this
    unfold thOf at this ⊢
    rw [hentry]
    simpa using this
  · intro T ha hi hprod
    rcases hinv.complete 0 (by omega) T ha (by simpa using hi) hprod with ⟨hg, hh⟩ | hd
    · left
      rw [h0] at hg hh
      unfold Holds at hh ⊢
      rw [hentry, hcur]
      simp only
      exact ⟨by simpa [g] using hh.1.trans hg, hh.2⟩
    · right; exact hd
  · intro sj hsj
    simp only [st1]
    rw [filterSlot_other st.bumpBoundaries si g sj (Ne.symm hsj)]
    rfl

/-- Facts about the state after the flags and the queue copy. -/
theorem copyQueue_fields (st : St) (a : List Nat) :
    (st.copyQueue a).so = st.so ∧ (st.copyQueue a).eo = st.eo ∧ (st.copyQueue a).ci = st.ci ∧
    (st.copyQueue a).matched = st.matched ∧ (st.copyQueue a).bestCtr = st.bestCtr ∧ (st.copyQueue a).bol = st.bol ∧
    (st.copyQueue a).eol = st.eol ∧ (st.copyQueue a).cur = st.cur := by
  unfold St.copyQueue; split <;> exact ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩

theorem flags_copy (st1 : St) (input : Input) (prev : Int) (active : List Nat) :
    let stC := (st1.setFlags input prev).copyQueue active
    (∀ sj, stC.slot sj = st1.slot sj) ∧ stC.queue = active.reverse ∧ stC.pos = st1.pos ∧ stC.ci = st1.ci ∧
    bestOf stC = bestOf st1 ∧ stC.matched = st1.matched ∧ stC.bestCtr = st1.bestCtr ∧ stC.slots.size = st1.slots.size ∧
    stC.bol = bolAt input st1.pos prev ∧
    stC.eol = ((st1.pos == input.bytes.size && !input.noteol) || (input.nlMode && curAt input st1.pos == 10)) ∧
    stC.cur = curAt input st1.pos := by
  intro stC
  obtain ⟨h1, h2, h3, h4, h5, h6, h7, h8⟩ := copyQueue_fields (st1.setFlags input prev) active
  refine ⟨fun sj => by simp [stC, St.slot], by simp [stC], by simp [stC], by rw [h3]; rfl, ?_, by rw [h4]; rfl,
    by rw [h5]; rfl, by simp [stC], by rw [h6]; rfl, by rw [h7]; rfl, by rw [h8]; rfl⟩
  show (stC.so, stC.bestCtr, stC.eo) = (st1.so, st1.bestCtr, st1.eo)
  simp only [stC, h1, h2, h5]; rfl

theorem paRelax_ci (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    (paRelax k st si pc start ctr).ci = st.ci := by
  simp only [paRelax]
  rcases paStore_cases k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr with
    h | ⟨h, _⟩ <;> rw [h] <;> rfl

theorem Dom_of_bestOf (k : Nat) (st st' : St) (hb : bestOf st' = bestOf st) (hm : st'.matched = st.matched) (v : Payload)
    (h : Dom k st v) : Dom k st' v := by
  rw [Dom_iff] at h ⊢
  simp only [bestOf, Prod.mk.injEq] at hb
  rw [hm, hb.1, hb.2.1]; exact h

/-- The state at the start of the closure: the invariant holds and every entry thread is covered. -/
theorem body_start (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (hring : 2 ≤ p.ring) (st1 : St)
    (prev : Int) (idx si : Nat) (active : List Nat)
    (hsize : st1.slots.size = p.ring) (hsi : si < p.ring) (hrs : RSlot p (st1.slot si))
    (hact : (st1.slot si).active = active) (hpos : st1.pos = chainPos input idx) (hvalid : ValidIdx input idx)
    (hprev : (prev = 10) ↔ (0 < idx ∧ curAt input (chainPos input (idx - 1)) = 10))
    (hsound : ∀ pc, pc < p.n → Fresh (st1.slot si) pc → Reach p atoms input (thOf idx (st1.slot si) pc))
    (hcomp : ∀ T, Arrived p atoms input idx T → T.i = idx → Prod p atoms input T →
      Holds p.k (st1.slot si) T.pc T.payload ∨ Dom p.k st1 T.payload)
    (hbestLen : st1.bestCtr.length = p.k)
    (hbestCand : st1.matched = true → Cand p atoms input st1.so st1.bestCtr st1.eo ∧ st1.eo < st1.pos) :
    let st2 := spawn p ((st1.setFlags input prev).copyQueue active) si
    CInvX p atoms input idx si st2 none ∧
    Covered p.k st2 si (spawnTh p input idx) ∧
    (∀ T, Arrived p atoms input idx T → T.i = idx → Prod p atoms input T → Covered p.k st2 si T) ∧
    bestOf st2 = bestOf st1 ∧ st2.matched = st1.matched ∧ st2.ci = st1.ci ∧ st2.pos = st1.pos ∧
    (∀ sj, sj ≠ si → st2.slot sj = st1.slot sj) ∧ st2.slots.size = st1.slots.size := by
  intro st2
  obtain ⟨hslot, hqueue, hposC, hci, hbest, hm, hbc, hsz, hbol, heol, hcur⟩ := flags_copy st1 input prev active
  have hsiC : si < ((st1.setFlags input prev).copyQueue active).slots.size := by rw [hsz, hsize]; exact hsi
  have hinvC : CInvX p atoms input idx si ((st1.setFlags input prev).copyQueue active) none := by
    refine ⟨by rw [hsz]; exact hsize, hsi, by rw [hslot]; exact hrs.size, by rw [hposC]; exact hpos, ?_, ?_, ?_,
      by rw [hbc]; exact hbestLen, by rw [hslot]; exact hrs.lens, by rw [hslot]; exact hsound, ?_, ?_, by rw [hslot]; exact hrs⟩
    · rw [hbol, hpos]; exact bol_eq_of_prevOK input idx prev hvalid hprev
    · rw [heol, hpos]
      unfold eolRef
      simp
    · intro pc hpc
      rw [hqueue, List.mem_reverse] at hpc
      exact (hrs.activeLt pc (hact ▸ hpc)).1
    · intro pc hpc hf _
      left
      rw [hqueue, List.mem_reverse, ← hact]
      rw [hslot] at hf
      exact hrs.freshActive pc hpc hf
    · intro hmC
      rw [hm] at hmC
      simp only [bestOf, Prod.mk.injEq] at hbest
      rw [hbest.1, hbest.2.1, hbest.2.2]
      exact (hbestCand hmC).1
  have hspawnReach : Reach p atoms input (spawnTh p input idx) := ⟨idx, hvalid, Steps.refl _⟩
  have hstart : p.start < p.n := hwf.1
  simp only [st2, spawn]
  split
  · rename_i hmC
    have hm1 : st1.matched = true := by rw [← hm]; exact hmC
    obtain ⟨hcand, heo1⟩ := hbestCand hm1
    have hsle : st1.so ≤ st1.eo := by
      obtain ⟨i, pc, hr, _, hpe⟩ := hcand
      have := Reach_start_le p atoms input _ hr
      simp only at this; omega
    refine ⟨hinvC, ?_, ?_, hbest, hm, hci, hposC, fun sj _ => hslot sj, hsz⟩
    · right
      rw [Dom_iff]
      refine ⟨hmC, Or.inl ?_⟩
      simp only [spawnTh, Th.payload]
      have hso : ((st1.setFlags input prev).copyQueue active).so = st1.so := by
        simp only [bestOf, Prod.mk.injEq] at hbest; exact hbest.1
      rw [hso, ← hpos]; omega
    · intro T ha hi hprod
      rcases hcomp T ha hi hprod with hh | hd
      · left; rw [hslot]; exact hh
      · right; exact Dom_of_bestOf p.k st1 _ hbest hm _ hd
  · rename_i hmC
    simp only [Bool.not_eq_true] at hmC
    have hposS : ((st1.setFlags input prev).copyQueue active).pos = chainPos input idx := by rw [hposC, hpos]
    rw [hposS]
    -- The spawn is a relaxation of the spawn payload at the start instruction.
    obtain ⟨hgen', hsize', hother', hself', hqueue', hslots', hsz', hqshape'⟩ :=
      paRelax_entries p.k ((st1.setFlags input prev).copyQueue active) si p.start (chainPos input idx)
        (List.replicate p.k 0) hsiC
    obtain ⟨hbest', hm', hpos', hbol', heol', hbc'⟩ :=
      paRelax_bestOf p.k ((st1.setFlags input prev).copyQueue active) si p.start (chainPos input idx)
        (List.replicate p.k 0)
    have hci' := paRelax_ci p.k ((st1.setFlags input prev).copyQueue active) si p.start (chainPos input idx)
      (List.replicate p.k 0)
    have hpost := paRelax_post p.k ((st1.setFlags input prev).copyQueue active) si p.start
      (chainPos input idx, List.replicate p.k 0) hsiC (by rw [hslot, hrs.size]; exact hstart)
      (fun _ => by rw [hslot, hrs.lens p.start hstart]; simp)
    have hDomFalse : ¬ Dom p.k ((st1.setFlags input prev).copyQueue active) (chainPos input idx, List.replicate p.k 0) := by
      intro hd; rw [Dom_iff] at hd; rw [hmC] at hd; simp at hd
    have hholdsSpawn : Holds p.k ((paRelax p.k ((st1.setFlags input prev).copyQueue active) si p.start (chainPos input idx)
        (List.replicate p.k 0)).slot si) p.start (chainPos input idx, List.replicate p.k 0) := by
      rcases hpost with h | h
      · exact h
      · exact absurd h hDomFalse
    have hentry : ∀ r, ((paRelax p.k ((st1.setFlags input prev).copyQueue active) si p.start (chainPos input idx)
        (List.replicate p.k 0)).slot si).entry r = (st1.slot si).entry r ∨
        (r = p.start ∧ ((paRelax p.k ((st1.setFlags input prev).copyQueue active) si p.start (chainPos input idx)
          (List.replicate p.k 0)).slot si).entry r =
          { stamp := (st1.slot si).gen, start := chainPos input idx, ctr := List.replicate p.k 0 } ∧
          r ∈ (paRelax p.k ((st1.setFlags input prev).copyQueue active) si p.start (chainPos input idx)
            (List.replicate p.k 0)).queue) := by
      intro r
      by_cases hr : r = p.start
      · subst hr
        rcases hself' with h | ⟨h, hmem⟩
        · left; rw [h, hslot]
        · right; exact ⟨rfl, by rw [h, hslot], hmem⟩
      · left; rw [hother' r hr, hslot]
    have hgenS : ((paRelax p.k ((st1.setFlags input prev).copyQueue active) si p.start (chainPos input idx)
        (List.replicate p.k 0)).slot si).gen = (st1.slot si).gen := by rw [hgen', hslot]
    refine ⟨?_, ?_, ?_, by rw [hbest', hbest], by rw [hm', hm], by rw [hci', hci], by rw [hpos', hposC],
      fun sj hsj => by rw [hslots' sj hsj, hslot], by rw [hsz', hsz]⟩
    · refine ⟨by rw [hsz', hsz]; exact hsize, hsi, by rw [hsize', hslot]; exact hrs.size, by rw [hpos', hposS],
        by rw [hbol', hbol, hpos]; exact bol_eq_of_prevOK input idx prev hvalid hprev,
        by rw [heol', heol, hpos]; unfold eolRef; simp, ?_, by rw [hbc', hbc]; exact hbestLen, ?_, ?_, ?_,
        fun hm2 => by rw [hm', hmC] at hm2; simp at hm2,
        RSlot_paRelax p _ si p.start (chainPos input idx) (List.replicate p.k 0) (by rw [hslot]; exact hrs) hsiC hstart (by simp)⟩
      · intro r hr
        rcases hqshape' with h | h
        · rw [h, hqueue, List.mem_reverse] at hr; exact (hrs.activeLt r (hact ▸ hr)).1
        · rw [h] at hr
          rcases List.mem_cons.mp hr with hr | hr
          · subst hr; exact hstart
          · rw [hqueue, List.mem_reverse] at hr; exact (hrs.activeLt r (hact ▸ hr)).1
      · intro r hr
        rcases hentry r with h | ⟨_, h, _⟩
        · rw [h]; exact hrs.lens r hr
        · rw [h]; simp
      · intro r hr hf
        rcases hentry r with h | ⟨hr', h, _⟩
        · unfold Fresh at hf; rw [h, hgenS] at hf
          have := hsound r hr hf
          unfold thOf at this ⊢; rw [h]; exact this
        · subst hr'
          unfold thOf; rw [h]
          exact hspawnReach
      · intro r hr hf _
        rcases hentry r with h | ⟨_, _, hmem⟩
        · unfold Fresh at hf; rw [h, hgenS] at hf
          left
          apply hqueue'
          rw [hqueue, List.mem_reverse, ← hact]
          exact hrs.freshActive r hr hf
        · exact Or.inl hmem
    · left; exact hholdsSpawn
    · intro T ha hi hprod
      have hpcT : T.pc < p.n := Reach_pc p atoms input hwf T (Arrived_reach p atoms input idx T ha)
      rcases hcomp T ha hi hprod with hh | hd
      · left
        exact Holds_paRelax p.k _ si p.start T.pc (chainPos input idx) (List.replicate p.k 0) T.payload hsiC
          (by rw [hslot, hrs.size]; exact hpcT) (by rw [hslot]; exact hh)
      · right
        exact Dom_of_bestOf p.k _ _ hbest' hm' _ (Dom_of_bestOf p.k st1 _ hbest hm _ hd)

/-! ## What the closure leaves alone: the other slots and the boundary index -/

theorem paConsider_ci (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (paConsider k st start ctr e).ci = st.ci := by
  simp only [paConsider]; unfold considerCore
  split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl

theorem handleOp_other (p : Prog) (si sj : Nat) (st : St) (pc start : Nat) (ctr : List Nat) (hne : sj ≠ si) :
    (handleOp p si st pc start ctr).slot sj = st.slot sj ∧ (handleOp p si st pc start ctr).ci = st.ci := by
  have hrel : ∀ (s1 : St) q, (paRelax p.k s1 si q start ctr).slot sj = s1.slot sj ∧
      (paRelax p.k s1 si q start ctr).ci = s1.ci := by
    intro s1 q
    refine ⟨?_, paRelax_ci _ _ _ _ _ _⟩
    simp only [paRelax]
    rcases paStore_cases p.k { s1 with m := { s1.m with relaxes := s1.m.relaxes + 1 } } si q start ctr with h | ⟨h, _⟩
    · rw [h]; rfl
    · rw [h]
      simp only [ite_true, St.slot_pushQueue, St.slot_charge]
      exact St.slot_setSlot_ne _ _ _ _ (Ne.symm hne)
  unfold handleOp
  split
  · rw [(hrel _ _).1, (hrel _ _).1, (hrel _ _).2, (hrel _ _).2]; exact ⟨rfl, rfl⟩
  · exact hrel _ _
  · split
    · exact hrel _ _
    · exact ⟨rfl, rfl⟩
  · split
    · exact hrel _ _
    · exact ⟨rfl, rfl⟩
  · exact ⟨paConsider_slot _ _ _ _ _ _, paConsider_ci _ _ _ _ _⟩
  · exact ⟨rfl, rfl⟩

theorem closureStep_ci (p : Prog) (st : St) (si : Nat) : (closureStep p st si).ci = st.ci := by
  unfold closureStep
  have hc : (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).ci = st.ci := by
    split <;> rfl
  generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = s1 at hc
  rw [← hc]
  unfold drain
  split
  · rfl
  · unfold handle
    split
    · rfl
    · exact (handleOp_other p si (si + 1) _ _ _ _ (by omega)).2

theorem paClosure_ci (p : Prog) (si : Nat) : ∀ (fuel : Nat) (st : St), (paClosure p si st fuel).ci = st.ci := by
  intro fuel
  induction fuel with
  | zero => intro st; rfl
  | succ fuel ih =>
    intro st
    simp only [paClosure]
    split
    · rfl
    · rw [ih, closureStep_ci]

theorem closureStep_other (p : Prog) (st : St) (si sj : Nat) (hne : sj ≠ si) :
    (closureStep p st si).slot sj = st.slot sj ∧ (closureStep p st si).ci = st.ci := by
  unfold closureStep
  have hc : (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).slot sj = st.slot sj ∧
      (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).ci = st.ci := by
    split <;> exact ⟨rfl, rfl⟩
  generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = s1 at hc
  rw [← hc.1, ← hc.2]
  unfold drain
  split
  · exact ⟨rfl, rfl⟩
  · unfold handle
    split
    · exact ⟨rfl, rfl⟩
    · exact handleOp_other p si sj _ _ _ _ hne

theorem paClosure_other (p : Prog) (si sj : Nat) (hne : sj ≠ si) :
    ∀ (fuel : Nat) (st : St), (paClosure p si st fuel).slot sj = st.slot sj ∧ (paClosure p si st fuel).ci = st.ci := by
  intro fuel
  induction fuel with
  | zero => intro st; exact ⟨rfl, rfl⟩
  | succ fuel ih =>
    intro st
    simp only [paClosure]
    split
    · exact ⟨rfl, rfl⟩
    · obtain ⟨h1, h2⟩ := ih (closureStep p st si)
      obtain ⟨h3, h4⟩ := closureStep_other p st si sj hne
      exact ⟨h1.trans h3, h2.trans h4⟩

theorem paClosure_slots_size (p : Prog) (si : Nat) :
    ∀ (fuel : Nat) (st : St), (paClosure p si st fuel).slots.size = st.slots.size := by
  intro fuel
  induction fuel with
  | zero => intro st; rfl
  | succ fuel ih =>
    intro st
    simp only [paClosure]
    split
    · rfl
    · rw [ih]
      unfold closureStep
      have hc : (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).slots.size = st.slots.size := by
        split <;> rfl
      generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = s1 at hc
      rw [← hc]
      unfold drain
      split
      · rfl
      · unfold handle
        split
        · rfl
        · have hrel : ∀ (s2 : St) q start ctr, (paRelax p.k s2 si q start ctr).slots.size = s2.slots.size := by
            intro s2 q start ctr
            simp only [paRelax]
            rcases paStore_cases p.k { s2 with m := { s2.m with relaxes := s2.m.relaxes + 1 } } si q start ctr with h | ⟨h, _⟩
            · rw [h]; rfl
            · rw [h]; simp
          unfold handleOp
          split
          · rw [hrel, hrel]; rfl
          · rw [hrel]; rfl
          · split
            · rw [hrel]; rfl
            · rfl
          · split
            · rw [hrel]; rfl
            · rfl
          · rw [paConsider_slots]; rfl
          · rfl

/-- The closure invariant gives the invariant of the potential argument. -/
theorem closureInv_of_cinv (p : Prog) (atoms : Atoms) (input : Input) (idx si : Nat) (st : St)
    (h : CInvX p atoms input idx si st none) (x : Payload) : ClosureInv p (knownPayloads x (st.slot si)) si st :=
  ⟨h.slotsSize, h.siLt, h.tableSize, h.queueLt, fun q hq hf =>
    fresh_mem_knownPayloads x (st.slot si) q (by rw [h.tableSize]; exact hq) hf⟩

/-- Draining the boundary: what holds afterwards. -/
theorem closure_result (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (hawf : atoms.wf2 p) (idx si : Nat)
    (st2 : St) (hvalid : ValidIdx input idx) (hinv : CInvX p atoms input idx si st2 none)
    (hspawn : Covered p.k st2 si (spawnTh p input idx))
    (harr : ∀ T, Arrived p atoms input idx T → T.i = idx → Prod p atoms input T → Covered p.k st2 si T) :
    let st3 := closeAt p si st2
    CInvX p atoms input idx si st3 none ∧ st3.queue = [] ∧
    (∀ T, Reach p atoms input T → T.i = idx → Prod p atoms input T → Covered p.k st3 si T) ∧
    (st2.matched = true → st3.matched = true ∧ selLE (bestOf st3) (bestOf st2)) ∧
    (∀ s c e, Cand p atoms input s c e → e = chainPos input idx → st3.matched = true ∧ selLE (bestOf st3) (s, c, e)) ∧
    st3.pos = st2.pos ∧ st3.ci = st2.ci ∧ (∀ sj, sj ≠ si → st3.slot sj = st2.slot sj) ∧ st3.slots.size = st2.slots.size := by
  intro st3
  have hn : 1 ≤ p.n := by have := hwf.1; omega
  obtain ⟨hinv3, hmono⟩ := paClosure_correct p atoms input hwf idx si (closureFuel p st2) st2 hinv
  have hempty : st3.queue = [] := by
    have hci := closureInv_of_cinv p atoms input idx si st2 hinv (chainPos input idx, List.replicate p.k 0)
    have hV : (knownPayloads (chainPos input idx, List.replicate p.k 0) (st2.slot si)).length ≤ p.n + 1 := by
      have := knownPayloads_length (chainPos input idx, List.replicate p.k 0) (st2.slot si)
      rw [hinv.tableSize] at this; exact this
    have hm := measure_le p _ si st2 hV
    exact (paClosure_bound p _ si hwf (closureFuel p st2) st2 hci).2.2 (by
      unfold closureFuel
      have : p.n * (p.n + 1) ≤ (p.n + 1) * (p.n + 1) := Nat.mul_le_mul_right _ (Nat.le_succ _)
      omega)
  -- Coverage persists through the closure.
  have hpersist : ∀ T : Th, T.pc < p.n → Covered p.k st2 si T → Covered p.k st3 si T := by
    intro T hpc hc
    have := paClosure_persist p atoms input hwf idx si T.pc T.payload hpc (closureFuel p st2) st2 hinv
    rcases hc with h | h
    · left; exact this.1 h
    · right; exact this.2 h
  have hclosed : ∀ T, Reach p atoms input T → T.i = idx → Prod p atoms input T → Covered p.k st3 si T := by
    intro T hT hi hprod
    obtain ⟨q, hq, hs⟩ := hT
    have hqi : q ≤ idx := by have := Steps_i p atoms input _ _ hs; simp [spawnTh] at this; omega
    by_cases hqe : q = idx
    · subst hqe
      exact closed_holds p atoms input hwf hawf q si st3 hinv3 hempty _ T hs rfl hi hwf.1 (by simp [spawnTh])
        (hpersist _ hwf.1 hspawn)
    · have hlt : q < idx := by omega
      obtain ⟨T0, delta, h1, h2, h3, h4, h5⟩ := Steps_cross p atoms input (idx - 1) _ T hs (by simp [spawnTh]; omega) (by omega)
      have hAi : T0.i + delta = idx := by
        have := Steps_i p atoms input _ _ h5
        simp only at this; omega
      have hT0 : Reach p atoms input T0 := ⟨q, hq, h1⟩
      have hA : Arrived p atoms input idx ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩ :=
        ⟨T0, delta, hT0, by omega, h3, rfl⟩
      have hprodA : Prod p atoms input ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩ :=
        Prod_of_steps p atoms input _ _ h5 hprod
      have hcovA := harr _ hA hAi hprodA
      have hpcA : p.next T0.pc < p.n := (hwf.2 T0.pc (Reach_pc p atoms input hwf T0 hT0)).1
      exact closed_holds p atoms input hwf hawf idx si st3 hinv3 hempty _ T h5 hAi hi hpcA
        (by simp only [bumpIf_length]; exact Reach_len p atoms input T0 hT0) (hpersist _ hpcA hcovA)
  refine ⟨hinv3, hempty, hclosed, hmono, ?_, paClosure_pos p si _ st2, paClosure_ci p si _ st2,
    fun sj hsj => (paClosure_other p si sj hsj _ st2).1, paClosure_slots_size p si _ st2⟩
  intro s c e hc he
  obtain ⟨i, pc, hr, hacc, hpe⟩ := hc
  have hi : i = idx := chainPos_inj input i idx (Reach_valid p atoms input hawf _ hr) hvalid (hpe.trans he)
  subst hi
  have hprod : Prod p atoms input ⟨i, pc, s, c⟩ := ⟨_, Steps.refl _, hacc⟩
  have hcov : Covered p.k st3 si ⟨i, pc, s, c⟩ := hclosed _ hr rfl hprod
  rcases hcov with hh | hd
  · have hpc := Reach_pc p atoms input hwf _ hr
    have hdone := closed_done p atoms input i si st3 hinv3 hempty pc hpc hh.1
    obtain ⟨hm3, hsel⟩ := hdone.2 hacc
    refine ⟨hm3, ?_⟩
    rw [he]
    refine selLE_trans _ _ _ hsel ?_
    have hl1 : (payloadAt (st3.slot si) pc).2.length = p.k := hinv3.lens pc hpc
    have hl2 : c.length = p.k := Reach_len p atoms input _ hr
    exact selLE_of_ple p.k _ (s, c) _ hl1 hl2 hh.2
  · rw [Dom_iff] at hd
    exact ⟨hd.1, selLE_of_Dom p.k st3 s c e (by rw [Dom_iff]; exact hd)⟩

/-! ## One arrival into a future slot -/

theorem mod_add_ne (ci delta r : Nat) (hr : 2 ≤ r) (hd : 1 ≤ delta) (hd2 : delta < r) : (ci + delta) % r ≠ ci % r := by
  rw [Nat.add_mod, Nat.mod_eq_of_lt hd2]
  have hlt : ci % r < r := Nat.mod_lt _ (by omega)
  by_cases h : ci % r + delta < r
  · rw [Nat.mod_eq_of_lt h]; omega
  · rw [Nat.mod_eq_sub_mod (by omega), Nat.mod_eq_of_lt (by omega)]; omega

theorem paStore_ci (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    (paStore k st si pc start ctr).2.ci = st.ci ∧ (paStore k st si pc start ctr).2.pos = st.pos ∧
    (paStore k st si pc start ctr).2.cur = st.cur ∧ (paStore k st si pc start ctr).2.slots.size = st.slots.size ∧
    (paStore k st si pc start ctr).2.ahead = st.ahead := by
  rcases paStore_cases k st si pc start ctr with h | ⟨h, _⟩ <;> rw [h] <;> simp [St.setSlot, St.charge]

/-- A reset slot for a new generation keeps the slot invariant when the new generation is above the old. -/
theorem RSlot_reset (p : Prog) (sl : Slot) (g : Nat) (h : RSlotQ p sl g) (hg : sl.gen < g) :
    RSlot p { sl with gen := g, active := [] } :=
  ⟨h.size, h.lens, fun pc hpc => by simp only; exact Nat.le_trans (h.stampLe pc hpc) (Nat.le_of_lt hg),
   fun pc hpc => by simp at hpc,
   fun pc hpc hf => by
     unfold Fresh at hf
     have := h.stampLe pc hpc
     simp only [Slot.entry_eq] at this hf
     omega⟩

/-- The effect of one arrival on its target slot, and on nothing else. -/
theorem paArrive_effect (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (st : St) (pc delta start : Nat)
    (ctr : List Nat) (hring : 2 ≤ p.ring) (hsize : st.slots.size = p.ring) (hd : 1 ≤ delta) (hd2 : delta < p.ring)
    (hpc : pc < p.n) (hl : ctr.length = p.k)
    (hrs : RSlotQ p (st.slot ((st.ci + delta) % p.ring)) (paGen (st.ci + delta))) :
    let st' := paArrive p st pc delta start ctr
    st'.ci = st.ci ∧ st'.pos = st.pos ∧ st'.cur = st.cur ∧ st'.slots.size = st.slots.size ∧
    bestOf st' = bestOf st ∧ st'.matched = st.matched ∧ st'.ahead = st.ahead ∧
    (∀ sj, sj ≠ (st.ci + delta) % p.ring → st'.slot sj = st.slot sj) ∧
    (st'.slot ((st.ci + delta) % p.ring)).gen = paGen (st.ci + delta) ∧ RSlot p (st'.slot ((st.ci + delta) % p.ring)) ∧
    (Holds p.k (st'.slot ((st.ci + delta) % p.ring)) (p.next pc) (start, bumpIf p pc ctr delta) ∨
      Dom p.k st (start, bumpIf p pc ctr delta)) ∧
    (∀ r w, r < p.n → (st.slot ((st.ci + delta) % p.ring)).gen = paGen (st.ci + delta) →
      Holds p.k (st.slot ((st.ci + delta) % p.ring)) r w → Holds p.k (st'.slot ((st.ci + delta) % p.ring)) r w) ∧
    (∀ r, r < p.n → Fresh (st'.slot ((st.ci + delta) % p.ring)) r →
      ((st'.slot ((st.ci + delta) % p.ring)).entry r = (st.slot ((st.ci + delta) % p.ring)).entry r ∧
        (st.slot ((st.ci + delta) % p.ring)).gen = paGen (st.ci + delta)) ∨
      (r = p.next pc ∧ (st'.slot ((st.ci + delta) % p.ring)).entry r =
        { stamp := paGen (st.ci + delta), start, ctr := bumpIf p pc ctr delta })) := by
  intro st'
  have hgen := hrs.genLe
  have hfi : (st.ci + delta) % p.ring < st.slots.size := by rw [hsize]; exact Nat.mod_lt _ (by omega)
  have hnext : (p.ins.getD pc default).next < p.n := (hwf.2 pc hpc).1
  have hnewCtr : (if p.k > 0 && !(p.ins.getD pc default).slots.isEmpty then bumpCtr ctr (p.ins.getD pc default).slots delta
      else ctr) = bumpIf p pc ctr delta := rfl
  simp only [st', paArrive]
  rw [hnewCtr]
  generalize hR : (if (({ st with m := { st.m with arrivals := st.m.arrivals + 1 } } : St).slot ((st.ci + delta) % p.ring)).gen !=
      paGen (st.ci + delta)
      then ({ st with m := { st.m with arrivals := st.m.arrivals + 1 } } : St).setSlot ((st.ci + delta) % p.ring)
        { ({ st with m := { st.m with arrivals := st.m.arrivals + 1 } } : St).slot ((st.ci + delta) % p.ring) with
          gen := paGen (st.ci + delta), active := [] }
      else ({ st with m := { st.m with arrivals := st.m.arrivals + 1 } } : St)) = stR
  have hRfacts : stR.ci = st.ci ∧ stR.pos = st.pos ∧ stR.cur = st.cur ∧ stR.slots.size = st.slots.size ∧
      bestOf stR = bestOf st ∧ stR.matched = st.matched ∧ stR.ahead = st.ahead ∧
      (∀ sj, sj ≠ (st.ci + delta) % p.ring → stR.slot sj = st.slot sj) ∧
      (stR.slot ((st.ci + delta) % p.ring)).gen = paGen (st.ci + delta) ∧ RSlot p (stR.slot ((st.ci + delta) % p.ring)) ∧
      ((st.slot ((st.ci + delta) % p.ring)).gen = paGen (st.ci + delta) →
        stR.slot ((st.ci + delta) % p.ring) = st.slot ((st.ci + delta) % p.ring)) ∧
      (∀ r, r < p.n → Fresh (stR.slot ((st.ci + delta) % p.ring)) r →
        (stR.slot ((st.ci + delta) % p.ring)).entry r = (st.slot ((st.ci + delta) % p.ring)).entry r ∧
        (st.slot ((st.ci + delta) % p.ring)).gen = paGen (st.ci + delta)) := by
    rw [← hR]
    split
    · rename_i hne
      simp only [St.slot_mk_m, bne_iff_ne, ne_eq] at hne
      simp only [St.slot_mk_m]
      have hlt : (st.slot ((st.ci + delta) % p.ring)).gen < paGen (st.ci + delta) := by omega
      have hslot : (({ st with m := { st.m with arrivals := st.m.arrivals + 1 } } : St).setSlot ((st.ci + delta) % p.ring)
          { st.slot ((st.ci + delta) % p.ring) with gen := paGen (st.ci + delta), active := [] }).slot ((st.ci + delta) % p.ring) =
          { st.slot ((st.ci + delta) % p.ring) with gen := paGen (st.ci + delta), active := [] } :=
        St.slot_setSlot_self _ _ _ (by simpa using hfi)
      refine ⟨rfl, rfl, rfl, by simp, rfl, rfl, rfl, fun sj hsj => St.slot_setSlot_ne _ _ _ _ (Ne.symm hsj), by rw [hslot],
        by rw [hslot]; exact RSlot_reset p _ _ hrs hlt, fun h => absurd h hne, ?_⟩
      intro r hr hf
      rw [hslot] at hf
      unfold Fresh at hf
      have := hrs.stampLe r hr
      simp only [Slot.entry_eq] at hf this
      omega
    · rename_i heq
      simp only [St.slot_mk_m, bne_iff_ne, ne_eq, Decidable.not_not] at heq
      refine ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, fun _ _ => rfl, heq, hrs.cur heq, fun _ => rfl, fun r hr hf => ⟨rfl, heq⟩⟩
  obtain ⟨hci, hpos, hcur, hsz, hbest, hm, hah, hother, hgenR, hrsR, hsame', hfreshR⟩ := hRfacts
  have hfiR : (st.ci + delta) % p.ring < stR.slots.size := by rw [hsz]; exact hfi
  obtain ⟨hgenS, hsizeS, hotherS, hselfS, hslotsS, hszS, hqS⟩ := paStore_entries p.k stR ((st.ci + delta) % p.ring)
    (p.ins.getD pc default).next start (bumpIf p pc ctr delta) hfiR
  obtain ⟨hbestS, hmS⟩ := paStore_bestOf p.k stR ((st.ci + delta) % p.ring) (p.ins.getD pc default).next start
    (bumpIf p pc ctr delta)
  obtain ⟨hciS, hposS, hcurS, hszS', hahS⟩ := paStore_ci p.k stR ((st.ci + delta) % p.ring) (p.ins.getD pc default).next
    start (bumpIf p pc ctr delta)
  have hpost := paStore_post p.k stR ((st.ci + delta) % p.ring) (p.ins.getD pc default).next (start, bumpIf p pc ctr delta)
    (by rw [hrsR.size]; exact hnext) hfiR (fun _ => by rw [hrsR.lens _ hnext]; simp [bumpIf_length, hl])
  refine ⟨hciS.trans hci, hposS.trans hpos, hcurS.trans hcur, hszS'.trans hsz, hbestS.trans hbest, hmS.trans hm,
    hahS.trans hah, fun sj hsj => (hslotsS sj hsj).trans (hother sj hsj), hgenS.trans hgenR,
    RSlot_paStore p.k p stR _ _ start _ hrsR hfiR hnext (by simp [bumpIf_length, hl]), ?_, ?_, ?_⟩
  · rcases hpost with h | h
    · exact Or.inl h
    · right; exact Dom_of_bestOf p.k stR st hbest.symm hm.symm _ h
  · intro r w hr hg hh
    rw [← hsame' hg] at hh
    exact Holds_paStore p.k stR _ _ r start _ w hfiR (by rw [hrsR.size]; exact hr) hh
  · intro r hr hf
    by_cases hrn : r = (p.ins.getD pc default).next
    · subst hrn
      rcases hselfS with h | ⟨h, _⟩
      · left
        unfold Fresh at hf
        rw [h, hgenS] at hf
        rw [h]
        exact hfreshR _ hr hf
      · right; exact ⟨rfl, by rw [h, hgenR]⟩
    · left
      rw [hotherS r hrn]
      unfold Fresh at hf
      rw [hotherS r hrn, hgenS] at hf
      exact hfreshR r hr hf

end PhaseA
end Vego
