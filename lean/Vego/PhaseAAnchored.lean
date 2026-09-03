/-
The anchored bound of phase A.

A program whose scan filter is enabled with an empty stop set reaches no consuming instruction from its
start with the anchors off, and it was compiled without newline mode.
A thread seeded past the first boundary therefore dies in its closure, and once the threads of the first
boundary are gone, the filter jumps to the end of the subject.
The threads of the first boundary live for at most as many boundaries as the longest consuming path of the
program, its depth.

`run_steps_le_anchored` proves `stepsFigureAnchored` for every program that carries the certificate
`Anchored`: labels that never drop along a split or jump edge and grow by one across a consuming
instruction, and the seed reach, closed under those edges and free of consuming instructions.

The proof carries a depth invariant across the boundary loop.
At the start of boundary `ci`, every fresh entry of its slot came from an arrival, so its label is at least
`ci`.
Inside the boundary, the seed and its epsilon reach join the fresh entries, and the consuming transitions
file arrivals whose labels are at least `ci + 1`.
Past the depth, nothing is fresh at the start of a boundary, so the boundary either jumps to the end of the
subject or is the end itself, and the run stops within two more boundaries.
-/

import Vego.PhaseACorrect

namespace Vego
namespace PhaseA

/-! ## The certificate check is sound -/

theorem anchoredCheck_sound (p : Prog) (d : Array Nat) (seed : Array Bool) (depth : Nat)
    (h : anchoredCheck p d seed depth = true) :
    Anchored p (fun q => d.getD q 0) (fun q => seed.getD q false) depth := by
  unfold anchoredCheck at h
  simp only [Bool.and_eq_true, Bool.not_eq_true', List.all_eq_true, List.mem_range, decide_eq_true_eq] at h
  obtain ⟨⟨⟨⟨hen, hsingle⟩, hstop⟩, hstart⟩, hins⟩ := h
  refine ⟨hen, hsingle, ?_, hstart, ?_, ?_, ?_, ?_⟩
  · intro b
    have hb : b.toNat < 256 := UInt8.toNat_lt b
    have := hstop b.toNat hb
    rwa [UInt8.ofNat_toNat] at this
  · intro pc hpc hop
    have hi := hins pc hpc
    rw [hop] at hi
    simp only [Bool.and_eq_true, decide_eq_true_eq, Bool.or_eq_true, Bool.not_eq_true'] at hi
    obtain ⟨⟨⟨h1, h2⟩, h3⟩, _⟩ := hi
    refine ⟨h1, h2, fun hs => ?_⟩
    rcases h3 with h3 | h3
    · rw [hs] at h3; exact absurd h3 (by decide)
    · exact h3
  · intro pc hpc hop
    have hi := hins pc hpc
    rw [hop] at hi
    simp only [Bool.and_eq_true, decide_eq_true_eq, Bool.or_eq_true, Bool.not_eq_true'] at hi
    obtain ⟨⟨h1, h3⟩, _⟩ := hi
    refine ⟨h1, fun hs => ?_⟩
    rcases h3 with h3 | h3
    · rw [hs] at h3; exact absurd h3 (by decide)
    · exact h3
  · intro pc hpc hop
    have hi := hins pc hpc
    have key : ∀ (o : Op), (p.ins.getD pc default).op = o → o.consuming = true →
        seed.getD pc false = false ∧ d.getD pc 0 + 1 ≤ d.getD (p.ins.getD pc default).next 0 := by
      intro o ho hc
      rw [ho] at hi
      cases o <;> first
        | exact absurd hc (by decide)
        | (simp only [Bool.and_eq_true, decide_eq_true_eq, Bool.not_eq_true'] at hi; exact ⟨hi.1.1, hi.1.2⟩)
    exact key _ rfl hop
  · intro pc hpc
    exact (hins pc hpc).2

/-! ## The entries a boundary may hold -/

/-- Every fresh entry of the slot satisfies `P`. -/
def FreshOK (p : Prog) (P : Nat → Prop) (sl : Slot) : Prop :=
  ∀ pc, pc < p.n → (sl.entry pc).stamp = sl.gen → P pc

/-- Where a fresh entry of boundary `c` may sit: on the seed reach, or on an instruction labeled at least `c`. -/
def depthOK (d : Nat → Nat) (seed : Nat → Bool) (c : Nat) (pc : Nat) : Prop := seed pc = true ∨ c ≤ d pc

theorem storeInto_freshOK (p : Prog) (P : Nat → Prop) (sl : Slot) (pc start : Nat) (ctr : List Nat)
    (hP : P pc) (h : FreshOK p P sl) : FreshOK p P (storeInto sl pc start ctr).1 := by
  intro q hq hf
  rw [storeInto_gen] at hf
  by_cases hqe : pc = q
  · subst hqe; exact hP
  · rw [storeInto_entry_ne _ _ _ _ _ hqe] at hf
    exact h q hq hf

/-- A store keeps every stamp of the slot at or below its generation. -/
theorem storeInto_stamps (p : Prog) (sl : Slot) (pc start : Nat) (ctr : List Nat) (hsize : sl.table.size = p.n)
    (h : ∀ q, q < p.n → (sl.entry q).stamp ≤ sl.gen) :
    ∀ q, q < p.n → ((storeInto sl pc start ctr).1.entry q).stamp ≤ (storeInto sl pc start ctr).1.gen := by
  intro q hq
  rw [storeInto_gen]
  by_cases hqe : pc = q
  · subst hqe
    rw [storeInto_entry_self _ _ _ _ (by rw [hsize]; exact hq)]
    exact Nat.le_refl _
  · rw [storeInto_entry_ne _ _ _ _ _ hqe]
    exact h q hq

/-! ## Fields the closure leaves alone -/

@[simp] theorem St.ci_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).ci = st.ci := rfl
@[simp] theorem St.bol_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).bol = st.bol := rfl
@[simp] theorem St.eol_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).eol = st.eol := rfl
@[simp] theorem St.pos_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).pos = st.pos := rfl
@[simp] theorem St.ci_charge (st : St) (b : Nat) : (st.charge b).ci = st.ci := rfl
@[simp] theorem St.bol_charge (st : St) (b : Nat) : (st.charge b).bol = st.bol := rfl
@[simp] theorem St.eol_charge (st : St) (b : Nat) : (st.charge b).eol = st.eol := rfl
@[simp] theorem St.pos_charge (st : St) (b : Nat) : (st.charge b).pos = st.pos := rfl
/-- The slot a store just wrote, seen through the charge of its growth. -/
theorem St.slot_charge_setSlot_self (st : St) (si : Nat) (sl : Slot) (b : Nat) (h : si < st.slots.size) :
    ((st.setSlot si sl).charge b).slot si = sl := by
  simp only [St.slot_charge]
  exact St.slot_setSlot_self _ _ _ h

/-! ## The closure keeps the depth property of the fresh entries -/

/--
The closure invariant at slot `si` of boundary `c`, with the scalars the closure leaves alone: the
position `q`, the generation `g` of the slot, and the anchor flags `b` and `e`.
Every other slot equals its value `others` before the closure.
-/
structure CloseInv (p : Prog) (P : Nat → Prop) (si c q g : Nat) (b e : Bool) (others : Nat → Slot) (st : St) :
    Prop where
  slotsSize : st.slots.size = p.ring
  tableSize : (st.slot si).table.size = p.n
  queueLt : ∀ pc ∈ st.queue, pc < p.n
  gen : (st.slot si).gen = g
  stamps : ∀ pc, pc < p.n → ((st.slot si).entry pc).stamp ≤ g
  fresh : FreshOK p P (st.slot si)
  other : ∀ sj, sj ≠ si → st.slot sj = others sj
  ci : st.ci = c
  pos : st.pos = q
  bol : st.bol = b
  eol : st.eol = e

variable {p : Prog} {P : Nat → Prop} {si c q g : Nat} {b e : Bool} {others : Nat → Slot} {st : St}

theorem CloseInv.of_same (h : CloseInv p P si c q g b e others st) (st' : St) (hs : st'.slots = st.slots)
    (hq : ∀ pc ∈ st'.queue, pc < p.n) (hci : st'.ci = st.ci) (hpos : st'.pos = st.pos) (hbol : st'.bol = st.bol)
    (heol : st'.eol = st.eol) : CloseInv p P si c q g b e others st' :=
  ⟨by rw [hs]; exact h.slotsSize, by simp only [St.slot, hs]; exact h.tableSize, hq,
   by simp only [St.slot, hs]; exact h.gen, by simp only [St.slot, hs]; exact h.stamps,
   by simp only [FreshOK, St.slot, hs]; exact h.fresh, fun sj hsj => by simp only [St.slot, hs]; exact h.other sj hsj,
   by rw [hci]; exact h.ci, by rw [hpos]; exact h.pos, by rw [hbol]; exact h.bol, by rw [heol]; exact h.eol⟩

theorem CloseInv.mk_m (h : CloseInv p P si c q g b e others st) (m : Meter) :
    CloseInv p P si c q g b e others { st with m := m } :=
  h.of_same _ rfl h.queueLt rfl rfl rfl rfl

theorem CloseInv.pushQueue (h : CloseInv p P si c q g b e others st) (pc : Nat) (hpc : pc < p.n) :
    CloseInv p P si c q g b e others (st.pushQueue pc) :=
  h.of_same _ rfl
    (fun x hx => by
      simp only [St.queue_pushQueue, List.mem_cons] at hx
      rcases hx with hx | hx
      · subst hx; exact hpc
      · exact h.queueLt x hx)
    rfl rfl rfl rfl

theorem CloseInv.popQueue (h : CloseInv p P si c q g b e others st) (pc : Nat) (rest : List Nat)
    (hq : st.queue = pc :: rest) : CloseInv p P si c q g b e others (st.popQueue rest) :=
  h.of_same _ rfl (fun x hx => h.queueLt x (by rw [hq]; exact List.mem_cons_of_mem _ hx)) rfl rfl rfl rfl

theorem CloseInv.compactQueue (h : CloseInv p P si c q g b e others st) :
    CloseInv p P si c q g b e others (compactQueue st) :=
  h.of_same _ rfl (fun x hx => h.queueLt x ((compactQueue_mem st x).mp hx)) rfl rfl rfl rfl

theorem CloseInv.paConsider (h : CloseInv p P si c q g b e others st) (start : Nat) (ctr : List Nat) (x : Nat) :
    CloseInv p P si c q g b e others (paConsider p.k st start ctr x) :=
  h.of_same _ (paConsider_slots _ _ _ _ _) (by simpa using h.queueLt) (paConsider_ci _ _ _ _ _)
    (paConsider_pos _ _ _ _ _) (paConsider_flags _ _ _ _ _).1 (paConsider_flags _ _ _ _ _).2

theorem CloseInv.slot_lt (h : CloseInv p P si c q g b e others st) (hsi : si < p.ring) : si < st.slots.size := by
  rw [h.slotsSize]; exact hsi

theorem paStore_close (h : CloseInv p P si c q g b e others st) (hsi : si < p.ring) (pc start : Nat)
    (ctr : List Nat) (hP : P pc) : CloseInv p P si c q g b e others (paStore p.k st si pc start ctr).2 := by
  rcases paStore_cases p.k st si pc start ctr with hc | ⟨hc, _⟩
  · rw [hc]; exact h
  · rw [hc]
    have hslot := St.slot_charge_setSlot_self st si (storeInto (st.slot si) pc start ctr).1
      (storeInto (st.slot si) pc start ctr).2 (h.slot_lt hsi)
    refine ⟨by simpa using h.slotsSize, ?_, by simpa using h.queueLt, ?_, ?_, ?_, ?_, by simpa using h.ci,
      by simpa using h.pos, by simpa using h.bol, by simpa using h.eol⟩
    · rw [hslot, storeInto_size]; exact h.tableSize
    · rw [hslot, storeInto_gen]; exact h.gen
    · rw [hslot]
      have := storeInto_stamps p (st.slot si) pc start ctr h.tableSize (by rw [h.gen]; exact h.stamps)
      rw [storeInto_gen, h.gen] at this
      exact this
    · rw [hslot]; exact storeInto_freshOK p P _ pc start ctr hP h.fresh
    · intro sj hsj
      simp only [St.slot_charge]
      rw [St.slot_setSlot_ne _ _ _ _ (Ne.symm hsj)]
      exact h.other sj hsj

theorem paRelax_close (h : CloseInv p P si c q g b e others st) (hsi : si < p.ring) (pc start : Nat)
    (ctr : List Nat) (hpc : pc < p.n) (hP : P pc) :
    CloseInv p P si c q g b e others (paRelax p.k st si pc start ctr) := by
  simp only [paRelax]
  have h0 := h.mk_m { st.m with relaxes := st.m.relaxes + 1 }
  rcases paStore_cases p.k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr with
    hc | ⟨hc, _⟩
  · rw [hc]; simpa using h0
  · rw [hc]
    simp only [ite_true]
    have := paStore_close h0 hsi pc start ctr hP
    rw [hc] at this
    exact this.pushQueue pc hpc

/-- What the closure needs from the depth property: closed under split and jump, and under the anchors that are on. -/
structure EdgeOK (p : Prog) (P : Nat → Prop) (b e : Bool) : Prop where
  split : ∀ pc, pc < p.n → (p.ins.getD pc default).op = .split → P pc →
    P (p.ins.getD pc default).next ∧ P (p.ins.getD pc default).alt
  jmp : ∀ pc, pc < p.n → (p.ins.getD pc default).op = .jmp → P pc → P (p.ins.getD pc default).next
  bol : b = true → ∀ pc, pc < p.n → (p.ins.getD pc default).op = .bol → P pc → P (p.ins.getD pc default).next
  eol : e = true → ∀ pc, pc < p.n → (p.ins.getD pc default).op = .eol → P pc → P (p.ins.getD pc default).next

theorem handleOp_close (hwf : p.wf) (hE : EdgeOK p P b e) (h : CloseInv p P si c q g b e others st)
    (hsi : si < p.ring) (pc start : Nat) (ctr : List Nat) (hpc : pc < p.n) (hP : P pc) :
    CloseInv p P si c q g b e others (handleOp p si st pc start ctr) := by
  have hnext := (hwf.2 pc hpc).1
  have halt := (hwf.2 pc hpc).2
  have hsplitE := fun hop => hE.split pc hpc hop hP
  have hjmpE := fun hop => hE.jmp pc hpc hop hP
  have hbolE := fun hb hop => hE.bol hb pc hpc hop hP
  have heolE := fun he hop => hE.eol he pc hpc hop hP
  clear hpc
  unfold handleOp
  split
  · rename_i hop
    obtain ⟨hn, ha⟩ := hsplitE hop
    exact paRelax_close (paRelax_close h hsi _ start ctr hnext hn) hsi _ start ctr halt ha
  · rename_i hop
    exact paRelax_close h hsi _ start ctr hnext (hjmpE hop)
  · rename_i hop
    split
    · rename_i hb
      have hb' : b = true := by rw [← h.bol]; exact hb
      exact paRelax_close h hsi _ start ctr hnext (hbolE hb' hop)
    · exact h
  · rename_i hop
    split
    · rename_i he
      have he' : e = true := by rw [← h.eol]; exact he
      exact paRelax_close h hsi _ start ctr hnext (heolE he' hop)
    · exact h
  · exact h.paConsider start ctr st.pos
  · exact h

theorem handle_close (hwf : p.wf) (hE : EdgeOK p P b e) (h : CloseInv p P si c q g b e others st)
    (hsi : si < p.ring) (pc : Nat) (hpc : pc < p.n) : CloseInv p P si c q g b e others (handle p si st pc) := by
  unfold handle
  split
  · exact h
  · rename_i hfresh
    simp only [bne_iff_ne, ne_eq, Decidable.not_not] at hfresh
    exact handleOp_close hwf hE h hsi pc _ _ hpc (h.fresh pc hpc hfresh)

theorem closureStep_close (hwf : p.wf) (hE : EdgeOK p P b e) (h : CloseInv p P si c q g b e others st)
    (hsi : si < p.ring) : CloseInv p P si c q g b e others (closureStep p st si) := by
  unfold closureStep
  have h1 : CloseInv p P si c q g b e others
      (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) := by
    split
    · exact h.compactQueue
    · exact h
  generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = st1 at h1 ⊢
  unfold drain
  split
  · exact h1
  · rename_i pc rest hq
    have hpc : pc < p.n := h1.queueLt pc (by rw [hq]; simp)
    exact handle_close hwf hE (h1.popQueue pc rest hq) hsi pc hpc

theorem paClosure_close (hwf : p.wf) (hE : EdgeOK p P b e) (hsi : si < p.ring) :
    ∀ (fuel : Nat) (st : St), CloseInv p P si c q g b e others st →
      CloseInv p P si c q g b e others (paClosure p si st fuel) := by
  intro fuel
  induction fuel with
  | zero => intro st h; simpa [paClosure] using h
  | succ fuel ih =>
    intro st h
    simp only [paClosure]
    split
    · exact h
    · exact ih _ (closureStep_close hwf hE h hsi)

theorem closeAt_close (hwf : p.wf) (hE : EdgeOK p P b e) (h : CloseInv p P si c q g b e others st)
    (hsi : si < p.ring) : CloseInv p P si c q g b e others (closeAt p si st) :=
  paClosure_close hwf hE hsi _ st h

theorem spawn_close (hwf : p.wf) (h : CloseInv p P si c q g b e others st) (hsi : si < p.ring)
    (hstart : P p.start) : CloseInv p P si c q g b e others (spawn p st si) := by
  unfold spawn
  split
  · exact h
  · exact paRelax_close h hsi _ _ _ hwf.1 hstart

/-! ## The consuming transitions file arrivals one label deeper -/

/--
The invariant of the consuming phase of boundary `c`.
The slot `si` of the boundary keeps its value `sl`, and the future slot `fi` only gains entries stamped with
the next generation, at instructions labeled at least `c + 1`.
-/
structure ConsInvD (p : Prog) (d : Nat → Nat) (si fi c : Nat) (sl : Slot) (st : St) : Prop where
  slotsSize : st.slots.size = p.ring
  cur : st.slot si = sl
  ci : st.ci = c
  futSize : (st.slot fi).table.size = p.n
  futGen : (st.slot fi).gen ≤ paGen (c + 1)
  futStamps : ∀ pc, pc < p.n → ((st.slot fi).entry pc).stamp ≤ paGen (c + 1)
  futFresh : ∀ pc, pc < p.n → ((st.slot fi).entry pc).stamp = paGen (c + 1) → c + 1 ≤ d pc

variable {d : Nat → Nat} {fi : Nat} {sl : Slot}

theorem ConsInvD.of_same (h : ConsInvD p d si fi c sl st) (st' : St) (hs : st'.slots = st.slots)
    (hci : st'.ci = st.ci) : ConsInvD p d si fi c sl st' :=
  ⟨by rw [hs]; exact h.slotsSize, by simp only [St.slot, hs]; exact h.cur, by rw [hci]; exact h.ci,
   by simp only [St.slot, hs]; exact h.futSize, by simp only [St.slot, hs]; exact h.futGen,
   by simp only [St.slot, hs]; exact h.futStamps, by simp only [St.slot, hs]; exact h.futFresh⟩

theorem ConsInvD.slot_lt (h : ConsInvD p d si fi c sl st) (hfi : fi < p.ring) : fi < st.slots.size := by
  rw [h.slotsSize]; exact hfi

/-- One arrival at `tgt`: the future slot gains a fresh entry there and nothing else changes. -/
theorem paStore_future (h : ConsInvD p d si fi c sl st) (hfi : fi < p.ring) (hne : si ≠ fi) (tgt start : Nat)
    (ctr : List Nat) (hgen : (st.slot fi).gen = paGen (c + 1)) (htgt : c + 1 ≤ d tgt) :
    ConsInvD p d si fi c sl (paStore p.k st fi tgt start ctr).2 := by
  rcases paStore_cases p.k st fi tgt start ctr with hc | ⟨hc, _⟩
  · rw [hc]; exact h
  · rw [hc]
    have hslot := St.slot_charge_setSlot_self st fi (storeInto (st.slot fi) tgt start ctr).1
      (storeInto (st.slot fi) tgt start ctr).2 (h.slot_lt hfi)
    have hcur : ((st.setSlot fi (storeInto (st.slot fi) tgt start ctr).1).charge
        (storeInto (st.slot fi) tgt start ctr).2).slot si = st.slot si := by
      simp only [St.slot_charge]
      exact St.slot_setSlot_ne _ _ _ _ (Ne.symm hne)
    refine ⟨by simpa using h.slotsSize, by rw [hcur]; exact h.cur, by simpa using h.ci, ?_, ?_, ?_, ?_⟩
    · rw [hslot, storeInto_size]; exact h.futSize
    · rw [hslot, storeInto_gen]; exact h.futGen
    · intro pc hpc
      rw [hslot]
      by_cases hqe : tgt = pc
      · subst hqe
        rw [storeInto_entry_self _ _ _ _ (by rw [h.futSize]; exact hpc), hgen]
        exact Nat.le_refl _
      · rw [storeInto_entry_ne _ _ _ _ _ hqe]; exact h.futStamps pc hpc
    · intro pc hpc hf
      rw [hslot] at hf
      by_cases hqe : tgt = pc
      · subst hqe; exact htgt
      · rw [storeInto_entry_ne _ _ _ _ _ hqe] at hf; exact h.futFresh pc hpc hf

theorem paArrive_future (h : ConsInvD p d si fi c sl st) (hfi : fi < p.ring) (hne : si ≠ fi)
    (hfi' : (c + 1) % p.ring = fi) (pc start : Nat) (ctr : List Nat)
    (hnext : c + 1 ≤ d (p.ins.getD pc default).next) :
    ConsInvD p d si fi c sl (paArrive p st pc 1 start ctr) := by
  -- The arrival, with its slot index and generation abstracted so that the let-bound names of `paArrive` unify.
  have key : ∀ (st1 : St) (fi1 g1 tgt : Nat) (ctr1 : List Nat), ConsInvD p d si fi c sl st1 → fi1 = fi →
      g1 = paGen (c + 1) → c + 1 ≤ d tgt →
      ConsInvD p d si fi c sl (paStore p.k (if (st1.slot fi1).gen != g1 then
        st1.setSlot fi1 { st1.slot fi1 with gen := g1, active := [] } else st1) fi1 tgt start ctr1).2 := by
    intro st1 fi1 g1 tgt ctr1 h1 hf hg htgt
    rw [hf, hg]
    have hreset : ConsInvD p d si fi c sl (if (st1.slot fi).gen != paGen (c + 1) then
          st1.setSlot fi { st1.slot fi with gen := paGen (c + 1), active := [] } else st1) ∧
        ((if (st1.slot fi).gen != paGen (c + 1) then
          st1.setSlot fi { st1.slot fi with gen := paGen (c + 1), active := [] } else st1).slot fi).gen =
          paGen (c + 1) := by
      split
      · have hslot : (st1.setSlot fi { st1.slot fi with gen := paGen (c + 1), active := [] }).slot fi =
            { st1.slot fi with gen := paGen (c + 1), active := [] } := St.slot_setSlot_self _ _ _ (h1.slot_lt hfi)
        refine ⟨⟨by simpa using h1.slotsSize, ?_, by simpa using h1.ci, ?_, ?_, ?_, ?_⟩, by rw [hslot]⟩
        · rw [St.slot_setSlot_ne _ _ _ _ (Ne.symm hne)]; exact h1.cur
        · rw [hslot]; exact h1.futSize
        · rw [hslot]; exact Nat.le_refl _
        · intro x hx; rw [hslot]; exact h1.futStamps x hx
        · intro x hx hf; rw [hslot] at hf; exact h1.futFresh x hx hf
      · rename_i hg
        simp only [bne_iff_ne, ne_eq, Decidable.not_not] at hg
        exact ⟨h1, hg⟩
    exact paStore_future hreset.1 hfi hne tgt start ctr1 hreset.2 htgt
  unfold paArrive
  exact key _ _ _ _ _ (h.of_same _ rfl rfl) (by rw [h.ci]; exact hfi') (by rw [h.ci]) hnext

theorem consumeArrive_future (atoms : Atoms) (h : ConsInvD p d si fi c sl st) (hfi : fi < p.ring) (hne : si ≠ fi)
    (hfi' : (c + 1) % p.ring = fi) (pc start : Nat) (ctr : List Nat)
    (hnext : c + 1 ≤ d (p.ins.getD pc default).next) :
    ConsInvD p d si fi c sl (consumeArrive p atoms st pc start ctr) := by
  unfold consumeArrive
  split
  · exact paArrive_future h hfi hne hfi' pc start ctr hnext
  · exact h

theorem consumeProbe_none (atoms : Atoms) (input : Input) (st : St) (ready : Bool) (pc start : Nat)
    (ctr : List Nat) (hlens : atoms.lens pc = []) : (consumeProbe p atoms input st ready pc start ctr).1 = st := by
  unfold consumeProbe
  rw [hlens]
  simp

theorem consumeFresh_future (atoms : Atoms) (input : Input) (h : ConsInvD p d si fi c sl st)
    (hfi : fi < p.ring) (hne : si ≠ fi) (hfi' : (c + 1) % p.ring = fi) (ready : Bool)
    (pc start : Nat) (ctr : List Nat) (hlens : atoms.lens pc = [])
    (hnext : (p.ins.getD pc default).op.consuming = true → c + 1 ≤ d (p.ins.getD pc default).next) :
    ConsInvD p d si fi c sl (consumeFresh p atoms input st ready pc start ctr).1 := by
  unfold consumeFresh
  split <;> try exact h
  all_goals
    rename_i hop
    rw [consumeProbe_none atoms input _ ready pc start ctr hlens]
    exact consumeArrive_future atoms h hfi hne hfi' pc start ctr (hnext (by rw [hop]; rfl))

theorem consumeOne_future (atoms : Atoms) (input : Input) (h : ConsInvD p d si fi c sl st)
    (hfi : fi < p.ring) (hne : si ≠ fi) (hfi' : (c + 1) % p.ring = fi) (ready : Bool)
    (pc : Nat) (hlens : atoms.lens pc = [])
    (hnext : (sl.entry pc).stamp = sl.gen → (p.ins.getD pc default).op.consuming = true →
      c + 1 ≤ d (p.ins.getD pc default).next) :
    ConsInvD p d si fi c sl (consumeOne p atoms input si st ready pc).1 := by
  unfold consumeOne
  split
  · exact h.of_same _ rfl rfl
  · rename_i hfresh
    simp only [bne_iff_ne, ne_eq, Decidable.not_not, h.cur] at hfresh
    exact consumeFresh_future atoms input (h.of_same st.bumpTests rfl rfl) hfi hne hfi' ready pc _ _ hlens
      (hnext hfresh)

theorem consumeList_future (atoms : Atoms) (input : Input) (hfi : fi < p.ring) (hne : si ≠ fi)
    (hfi' : (c + 1) % p.ring = fi)
    (hnext : ∀ pc, pc < p.n → (sl.entry pc).stamp = sl.gen → (p.ins.getD pc default).op.consuming = true →
      c + 1 ≤ d (p.ins.getD pc default).next) :
    ∀ (l : List Nat) (st : St) (ready : Bool), (∀ pc ∈ l, pc < p.n) → (∀ pc ∈ l, atoms.lens pc = []) →
      ConsInvD p d si fi c sl st → ConsInvD p d si fi c sl (consumeList p atoms input si l st ready) := by
  intro l
  induction l with
  | nil => intro st ready _ _ h; simpa [consumeList] using h
  | cons pc rest ih =>
    intro st ready hlt hlens h
    simp only [consumeList]
    exact ih _ _ (fun x hx => hlt x (List.mem_cons_of_mem _ hx))
      (fun x hx => hlens x (List.mem_cons_of_mem _ hx))
      (consumeOne_future atoms input h hfi hne hfi' ready pc (hlens pc (List.mem_cons_self ..))
        (hnext pc (hlt pc (List.mem_cons_self ..))))

theorem paConsume_future (atoms : Atoms) (input : Input) (hawf : atoms.wf p) (hring : p.ring = 2)
    (hfi : fi < p.ring) (hne : si ≠ fi) (hfi' : (c + 1) % p.ring = fi) (h : ConsInvD p d si fi c sl st)
    (hlt : ∀ pc ∈ sl.active, pc < p.n)
    (hnext : ∀ pc, pc < p.n → (sl.entry pc).stamp = sl.gen → (p.ins.getD pc default).op.consuming = true →
      c + 1 ≤ d (p.ins.getD pc default).next) :
    ConsInvD p d si fi c sl (paConsume p atoms input st si) := by
  unfold paConsume
  rw [h.cur]
  apply consumeList_future atoms input hfi hne hfi' hnext sl.active st false hlt _ h
  intro pc _
  have := hawf pc
  rw [hring] at this
  exact List.eq_nil_of_length_eq_zero (by omega)

/-! ## The scan filter with an empty stop set jumps to the end -/

theorem scanAheadFrom_empty (p : Prog) (input : Input) (hsingle : p.scan.single = false)
    (hstop : ∀ b, p.scan.stop b = false) :
    ∀ (fuel i : Nat), i ≤ input.bytes.size → input.bytes.size - i ≤ fuel →
      (scanAheadFrom p input i fuel).1 = input.bytes.size := by
  intro fuel
  induction fuel with
  | zero => intro i hi hf; simp only [scanAheadFrom]; omega
  | succ fuel ih =>
    intro i hi hf
    simp only [scanAheadFrom]
    split
    · rename_i hlt
      simp only [hsingle, hstop, Bool.false_and, Bool.not_false, Bool.true_and, Bool.false_or,
        Bool.false_eq_true, ite_false]
      exact ih (i + 1) (by omega) (by omega)
    · omega

theorem scanAhead_empty (p : Prog) (input : Input) (hsingle : p.scan.single = false)
    (hstop : ∀ b, p.scan.stop b = false) (pos : Nat) (hpos : pos ≤ input.bytes.size) :
    (scanAhead p input pos).1 = input.bytes.size :=
  scanAheadFrom_empty p input hsingle hstop _ pos hpos (by omega)

theorem bolAt_false (input : Input) (pos : Nat) (prev : Int) (hnl : input.nlMode = false) (hpos : 0 < pos) :
    bolAt input pos prev = false := by
  simp [bolAt, hnl, Nat.ne_of_gt hpos]

/-! ## The depth invariant of the boundary loop -/

/--
The depth invariant at the start of a boundary.
No stamp or generation is past the boundary, every fresh entry of its slot is labeled at least `ci`, the
boundary index never passes the position, and the boundary count stays within `depth + 2`, the last one
being the end of the subject.
-/
structure DepthInv (p : Prog) (d : Nat → Nat) (depth : Nat) (input : Input) (st : St) : Prop where
  stamps : ∀ si, si < p.ring → ∀ pc, pc < p.n → ((st.slot si).entry pc).stamp ≤ paGen st.ci
  gens : ∀ si, si < p.ring → (st.slot si).gen ≤ paGen st.ci
  live : ∀ pc, pc < p.n → ((st.slot (st.ci % p.ring)).entry pc).stamp = paGen st.ci → st.ci ≤ d pc
  ciPos : st.ci ≤ st.pos
  ciDepth : st.ci ≤ depth + 2
  atEnd : st.ci = depth + 2 → st.pos = input.bytes.size

@[simp] theorem St.ci_filterSlot (st : St) (si g : Nat) : (st.filterSlot si g).ci = st.ci := rfl
@[simp] theorem St.matched_filterSlot (st : St) (si g : Nat) : (st.filterSlot si g).matched = st.matched := rfl
@[simp] theorem St.ci_bumpBoundaries (st : St) : st.bumpBoundaries.ci = st.ci := rfl
@[simp] theorem St.matched_bumpBoundaries (st : St) : st.bumpBoundaries.matched = st.matched := rfl
@[simp] theorem St.ci_bumpSkipped (st : St) (n : Nat) : (st.bumpSkipped n).ci = st.ci := rfl
@[simp] theorem St.ci_jumpTo (st : St) (n : Nat) : (st.jumpTo n).ci = st.ci + 1 := rfl
@[simp] theorem St.ci_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).ci = st.ci := rfl
@[simp] theorem St.ci_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).ci = st.ci :=
  (copyQueue_fields st a).2.2.1
@[simp] theorem St.bol_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).bol = bolAt i st.pos v := rfl
@[simp] theorem St.bol_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).bol = st.bol :=
  (copyQueue_fields st a).2.2.2.2.2.1
@[simp] theorem St.eol_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).eol = st.eol :=
  (copyQueue_fields st a).2.2.2.2.2.2.1

theorem St.eol_setFlags_false (st : St) (i : Input) (v : Int) (hnl : i.nlMode = false)
    (hne : st.pos ≠ i.bytes.size) : (st.setFlags i v).eol = false := by
  simp [St.setFlags, hnl, hne]

/-- The pending test fails when the future slot does not carry the next generation. -/
theorem pendingFrom_none (p : Prog) (st : St) (hring : p.ring = 2)
    (hgen : (st.slot ((st.ci + 1) % p.ring)).gen ≠ paGen (st.ci + 1)) :
    (pendingFrom p st 1 p.ring).1 = false := by
  rw [hring] at hgen
  simp [pendingFrom, hring, hgen]

/-- With ring two, every slot is the slot of the boundary or the future slot. -/
theorem slot_cases (p : Prog) (c sj : Nat) (hring : p.ring = 2) (hsj : sj < p.ring) :
    sj = c % p.ring ∨ sj = (c + 1) % p.ring := by
  rw [hring] at hsj ⊢
  omega

/--
The body of a boundary that is not the end of the subject.
An idle boundary, with nothing live and a match already known, ends the run.
A boundary that continues carries the depth facts to the next one.
-/
theorem boundaryBody_depth (p : Prog) (atoms : Atoms) (input : Input) (st : St) (si : Nat) (prev : Int)
    (active : List Nat) (d : Nat → Nat) (seed : Nat → Bool) (depth : Nat)
    (hwf : p.wf) (hawf : atoms.wf p) (hring : p.ring = 2) (hnl : input.nlMode = false)
    (ha : Anchored p d seed depth) (hinv : GlobalInv p st) (hsi : si = st.ci % p.ring)
    (hactive : active = (st.slot si).active) (hgen : (st.slot si).gen = paGen st.ci)
    (hfresh : ∀ pc, pc < p.n → ((st.slot si).entry pc).stamp = paGen st.ci → st.ci ≤ d pc)
    (hstamps : ∀ sj, sj < p.ring → ∀ pc, pc < p.n → ((st.slot sj).entry pc).stamp ≤ paGen st.ci)
    (hgens : ∀ sj, sj < p.ring → (st.slot sj).gen ≤ paGen st.ci)
    (hcipos : st.ci ≤ st.pos) (hpos : st.pos < input.bytes.size) :
    let r := boundaryBody p atoms input st si prev active
    (active = [] → st.matched = true → r.2 = none) ∧
    (r.2.isSome → r.1.ci = st.ci + 1 ∧ st.pos < r.1.pos ∧
      (∀ sj, sj < p.ring → ∀ pc, pc < p.n → ((r.1.slot sj).entry pc).stamp ≤ paGen (st.ci + 1)) ∧
      (∀ sj, sj < p.ring → (r.1.slot sj).gen ≤ paGen (st.ci + 1)) ∧
      (∀ pc, pc < p.n → ((r.1.slot ((st.ci + 1) % p.ring)).entry pc).stamp = paGen (st.ci + 1) →
        st.ci + 1 ≤ d pc)) := by
  intro r
  have hsi' : si < p.ring := by rw [hsi]; exact Nat.mod_lt _ (by omega)
  have hfi' : (st.ci + 1) % p.ring < p.ring := Nat.mod_lt _ (by omega)
  have hne : si ≠ (st.ci + 1) % p.ring := by rw [hsi, hring]; omega
  have hn : 1 ≤ p.n := by have := hwf.1; omega
  have hlt : ∀ pc ∈ active, pc < p.n := by
    intro pc hpc; rw [hactive] at hpc; exact ((hinv.slots si hsi').mem pc hpc).1
  have hlen : active.length ≤ p.n := by rw [hactive]; exact (hinv.slots si hsi').active_len
  -- The state before the closure.
  have hslots2 : ∀ sj, ((st.setFlags input prev).copyQueue active).slot sj = st.slot sj := fun sj => by simp
  have hsize2 : ((st.setFlags input prev).copyQueue active).slots.size = p.ring := by simp [hinv.slotsSize]
  have heol2 : ((st.setFlags input prev).copyQueue active).eol = false := by
    rw [St.eol_copyQueue]
    exact St.eol_setFlags_false _ _ _ hnl (Nat.ne_of_lt hpos)
  have hqueue2 : ∀ pc ∈ ((st.setFlags input prev).copyQueue active).queue, pc < p.n := by
    intro pc hpc
    simp only [St.queue_copyQueue, List.mem_reverse] at hpc
    exact hlt pc hpc
  -- The depth property the closure keeps.
  have hE : EdgeOK p (depthOK d seed st.ci) ((st.setFlags input prev).copyQueue active).bol
      ((st.setFlags input prev).copyQueue active).eol := by
    refine ⟨?_, ?_, ?_, ?_⟩
    · intro pc hpc hop hP
      obtain ⟨h1, h2, h3⟩ := ha.split pc hpc hop
      rcases hP with hs | hd
      · exact ⟨Or.inl (h3 hs).1, Or.inl (h3 hs).2⟩
      · exact ⟨Or.inr (Nat.le_trans hd h1), Or.inr (Nat.le_trans hd h2)⟩
    · intro pc hpc hop hP
      obtain ⟨h1, h2⟩ := ha.jmp pc hpc hop
      rcases hP with hs | hd
      · exact Or.inl (h2 hs)
      · exact Or.inr (Nat.le_trans hd h1)
    · intro hb pc _ _ _
      -- The start anchor is on at the first boundary only, and there every label passes.
      rw [St.bol_copyQueue, St.bol_setFlags] at hb
      by_cases hz : st.ci = 0
      · rw [hz]; exact Or.inr (Nat.zero_le _)
      · rw [bolAt_false input st.pos prev hnl (by omega)] at hb
        exact absurd hb (by decide)
    · intro he
      rw [heol2] at he
      exact absurd he (by decide)
  have hclose2 : CloseInv p (depthOK d seed st.ci) si st.ci st.pos (paGen st.ci)
      ((st.setFlags input prev).copyQueue active).bol ((st.setFlags input prev).copyQueue active).eol
      (fun sj => st.slot sj) ((st.setFlags input prev).copyQueue active) :=
    ⟨hsize2, by rw [hslots2]; exact (hinv.slots si hsi').size, hqueue2, by rw [hslots2]; exact hgen,
     by rw [hslots2]; exact hstamps si hsi',
     fun pc hpc hf => by
       rw [hslots2, hgen] at hf
       exact Or.inr (hfresh pc hpc hf),
     fun sj _ => hslots2 sj, by simp, by simp, rfl, rfl⟩
  have hclose4 := closeAt_close hwf hE (spawn_close hwf hclose2 hsi' (Or.inl ha.start)) hsi'
  -- The closure keeps the global invariant, which bounds the live list it leaves.
  have hinv4 : GlobalInv p (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)) := by
    have h1 : GlobalInv p (st.setFlags input prev) := hinv.of_same _ rfl rfl rfl rfl rfl rfl
    have h2 := copyQueue_step p _ active h1 hlt hlen
    obtain ⟨h3, _, _, _⟩ := spawn_step p _ si 0 hwf h2 hsi' (by simp [List.length_reverse]; omega)
    exact (closeAt_step p _ si 0 hwf h3 hsi' hn).1
  -- The consuming phase, from the slot the closure left.
  have hcons4 : ConsInvD p d si ((st.ci + 1) % p.ring) st.ci
      ((closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)).slot si)
      (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)) := by
    have hfut := hclose4.other _ (Ne.symm hne)
    refine ⟨hclose4.slotsSize, rfl, hclose4.ci, ?_, ?_, ?_, ?_⟩
    · rw [hfut]; exact (hinv.slots _ hfi').size
    · rw [hfut]; exact Nat.le_trans (hgens _ hfi') (by simp only [paGen]; omega)
    · intro pc hpc; rw [hfut]; exact Nat.le_trans (hstamps _ hfi' pc hpc) (by simp only [paGen]; omega)
    · intro pc hpc hf
      rw [hfut] at hf
      have := hstamps _ hfi' pc hpc
      rw [hf] at this
      simp only [paGen] at this
      omega
  have hcons5 := paConsume_future atoms input hawf hring hfi' hne rfl hcons4
    (fun pc hpc => ((hinv4.slots si hsi').mem pc hpc).1)
    (fun pc hpc hf hop => by
      have hP := hclose4.fresh pc hpc hf
      obtain ⟨hs, hd⟩ := ha.consume pc hpc hop
      rcases hP with hP | hP
      · rw [hs] at hP; exact absurd hP (by decide)
      · omega)
  -- The two claims.
  simp only [r, boundaryBody]
  rw [if_neg (by simp only [beq_iff_eq]; exact Nat.ne_of_lt hpos)]
  refine ⟨?_, ?_⟩
  · intro hnil hmatched
    -- Nothing is queued and nothing is live, so the boundary does nothing and the pending test fails.
    have hq : ((st.setFlags input prev).copyQueue active).queue = [] := by simp [hnil]
    have hsp : spawn p ((st.setFlags input prev).copyQueue active) si = (st.setFlags input prev).copyQueue active := by
      unfold spawn
      rw [if_pos (by simp [hmatched])]
    have hcl : closeAt p si ((st.setFlags input prev).copyQueue active) = (st.setFlags input prev).copyQueue active := by
      unfold closeAt
      have : closureFuel p ((st.setFlags input prev).copyQueue active) =
          (((st.setFlags input prev).copyQueue active).queue.length + (p.n + 1) * (p.n + 1)) + 1 := rfl
      rw [this]
      simp only [paClosure]
      rw [if_pos (by simp [hnil])]
    have hco : paConsume p atoms input ((st.setFlags input prev).copyQueue active) si =
        (st.setFlags input prev).copyQueue active := by
      unfold paConsume
      rw [hslots2, ← hactive, hnil]
      rfl
    rw [hsp, hcl, hco]
    unfold afterConsume
    rw [if_pos (by simp [hmatched])]
    have hpend : (pendingFrom p ((st.setFlags input prev).copyQueue active) 1 p.ring).1 = false := by
      apply pendingFrom_none p _ hring
      rw [St.ci_copyQueue, St.ci_setFlags, hslots2]
      have := hgens _ hfi'
      simp only [paGen] at this ⊢
      omega
    rw [hpend]
    simp
  · intro hsome
    have hsize : 1 ≤ (decodeRuneAt input.bytes st.pos).2 := (decodeRuneAt_size input.bytes st.pos hpos).1
    -- The continuation advances, and its slots are those of the consuming phase.
    have hadv : ∀ (s : St) (size : Nat), (afterConsume p s size).2.isSome →
        (afterConsume p s size).1.ci = s.ci + 1 ∧ (afterConsume p s size).1.slots = s.slots ∧
        (afterConsume p s size).1.pos = s.pos + size := by
      intro s size hs
      unfold afterConsume at hs ⊢
      by_cases hm : s.matched = true
      · rw [if_pos hm] at hs ⊢
        by_cases hp : (pendingFrom p s 1 p.ring).1 = true
        · rw [if_pos hp] at hs ⊢; exact ⟨rfl, rfl, rfl⟩
        · rw [if_neg hp] at hs; simp at hs
      · rw [if_neg hm] at hs ⊢; exact ⟨rfl, rfl, rfl⟩
    obtain ⟨hci', hslots', hpos'⟩ := hadv _ _ hsome
    have hsl : ∀ sj, (afterConsume p (paConsume p atoms input
        (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)) si)
        (decodeRuneAt input.bytes st.pos).2).1.slot sj = (paConsume p atoms input
        (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)) si).slot sj := by
      intro sj
      simp only [St.slot, hslots']
    have hpos5 : (paConsume p atoms input (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)) si).pos = st.pos := by
      rw [paConsume_pos, hclose4.pos]
    refine ⟨by rw [hci', hcons5.ci], by rw [hpos', hpos5]; omega, ?_, ?_, ?_⟩
    · intro sj hsj pc hpc
      rw [hsl]
      rcases slot_cases p st.ci sj hring hsj with h | h
      · rw [h, ← hsi, hcons5.cur]
        exact Nat.le_trans (hclose4.stamps pc hpc) (by simp only [paGen]; omega)
      · rw [h]; exact hcons5.futStamps pc hpc
    · intro sj hsj
      rw [hsl]
      rcases slot_cases p st.ci sj hring hsj with h | h
      · rw [h, ← hsi, hcons5.cur, hclose4.gen]; simp only [paGen]; omega
      · rw [h]; exact hcons5.futGen
    · intro pc hpc hf
      rw [hsl] at hf
      exact hcons5.futFresh pc hpc hf

/-- One boundary of the loop keeps the depth invariant when the run continues. -/
theorem boundaryStep_depth (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int)
    (d : Nat → Nat) (seed : Nat → Bool) (depth : Nat)
    (hwf : p.wf) (hawf : atoms.wf p) (hring : p.ring = 2) (hnl : input.nlMode = false)
    (ha : Anchored p d seed depth) (hinv : GlobalInv p st) (hd : DepthInv p d depth input st)
    (hpos : st.pos ≤ input.bytes.size) :
    let r := boundaryStep p atoms input st prev
    r.2.isSome → DepthInv p d depth input r.1 ∧ r.1.ci = st.ci + 1 := by
  intro r hsome
  have hsi : st.ci % p.ring < p.ring := Nat.mod_lt _ (by omega)
  have hfi : (st.ci + 1) % p.ring < p.ring := Nat.mod_lt _ (by omega)
  have hb : GlobalInv p st.bumpBoundaries := hinv.of_same _ rfl rfl rfl rfl rfl rfl
  have hinv0 := (filterSlot_step p st.bumpBoundaries (st.ci % p.ring) (paGen st.ci) 0 hb hsi).1
  have hself := filterSlot_current st.bumpBoundaries (st.ci % p.ring) (paGen st.ci) (hb.slot_lt hsi)
  have hother : ∀ sj, sj ≠ st.ci % p.ring →
      (st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)).slot sj = st.slot sj := by
    intro sj hsj
    rw [filterSlot_other _ _ _ _ (Ne.symm hsj)]
    rfl
  have hci0 : (st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)).ci = st.ci := rfl
  have hpos0 : (st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)).pos = st.pos := rfl
  -- The facts of the opened slot.
  have hstamps0 : ∀ sj, sj < p.ring → ∀ pc, pc < p.n →
      (((st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)).slot sj).entry pc).stamp ≤ paGen st.ci := by
    intro sj hsj pc hpc
    by_cases h : sj = st.ci % p.ring
    · rw [h, hself]; exact hd.stamps _ hsi pc hpc
    · rw [hother sj h]; exact hd.stamps sj hsj pc hpc
  have hgens0 : ∀ sj, sj < p.ring →
      ((st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)).slot sj).gen ≤ paGen st.ci := by
    intro sj hsj
    by_cases h : sj = st.ci % p.ring
    · rw [h, hself]; exact Nat.le_refl _
    · rw [hother sj h]; exact hd.gens sj hsj
  have hfresh0 : ∀ pc, pc < p.n →
      (((st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)).slot (st.ci % p.ring)).entry pc).stamp =
        paGen st.ci → st.ci ≤ d pc := by
    intro pc hpc hf
    rw [hself] at hf
    exact hd.live pc hpc hf
  -- Past the depth, nothing is live at the start of the boundary.
  have hidle : depth < st.ci → liveAt st (st.ci % p.ring) (paGen st.ci) = [] := by
    intro hgt
    unfold liveAt
    rw [List.filter_eq_nil_iff]
    intro pc hpc
    have hlt := ((hinv.slots _ hsi).mem pc hpc).1
    simp only [beq_iff_eq]
    intro hf
    have := hd.live pc hlt hf
    have := ha.bound pc hlt
    omega
  have hcile : st.ci ≤ depth + 1 ∨ st.pos = input.bytes.size := by
    have := hd.ciDepth
    by_cases h : st.ci = depth + 2
    · exact Or.inr (hd.atEnd h)
    · exact Or.inl (by omega)
  have hscan := scanAhead_empty p input ha.single ha.stop st.pos hpos
  -- The boundary after the filter.
  simp only [r, boundaryStep] at hsome ⊢
  unfold boundaryAfterFilter at hsome ⊢
  split at hsome
  · rename_i hcond
    rw [if_pos hcond]
    have hcond' := hcond
    simp only [Bool.and_eq_true, List.isEmpty_iff, Bool.not_eq_true', decide_eq_true_eq, hpos0] at hcond'
    obtain ⟨⟨⟨⟨_, _⟩, _⟩, hlt⟩, _⟩ := hcond'
    have hjump : (scanAhead p input (st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)).pos).1 >
        (st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)).pos := by
      rw [hpos0, hscan]; exact hlt
    rw [if_pos hjump]
    -- The jump: one more boundary, at the end of the subject, with nothing live.
    have hci1 : st.ci ≤ depth + 1 := by
      rcases hcile with h | h
      · exact h
      · omega
    refine ⟨⟨?_, ?_, ?_, ?_, ?_, ?_⟩, rfl⟩
    · intro sj hsj pc hpc
      simp only [St.slot_jumpTo, St.slot_bumpSkipped, St.ci_jumpTo, St.ci_bumpSkipped, St.ci_filterSlot,
        St.ci_bumpBoundaries]
      exact Nat.le_trans (hstamps0 sj hsj pc hpc) (by simp only [paGen]; omega)
    · intro sj hsj
      simp only [St.slot_jumpTo, St.slot_bumpSkipped, St.ci_jumpTo, St.ci_bumpSkipped, St.ci_filterSlot,
        St.ci_bumpBoundaries]
      exact Nat.le_trans (hgens0 sj hsj) (by simp only [paGen]; omega)
    · intro pc hpc hf
      simp only [St.slot_jumpTo, St.slot_bumpSkipped, St.ci_jumpTo, St.ci_bumpSkipped, St.ci_filterSlot,
        St.ci_bumpBoundaries] at hf
      have := hstamps0 _ hfi pc hpc
      rw [hf] at this
      simp only [paGen] at this
      omega
    · show st.ci + 1 ≤ (scanAhead p input st.pos).1
      rw [hscan]
      have := hd.ciPos
      omega
    · show st.ci + 1 ≤ depth + 2
      omega
    · intro _
      show (scanAhead p input st.pos).1 = input.bytes.size
      exact hscan
  · rename_i hcond
    rw [if_neg hcond]
    -- The body: the end returns, and a continuation is one more boundary with the arrivals it filed.
    by_cases hend : st.pos = input.bytes.size
    · exfalso
      have hnone : (boundaryBody p atoms input (st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci))
          (st.ci % p.ring) prev (liveAt st (st.ci % p.ring) (paGen st.ci))).2 = none := by
        unfold boundaryBody
        rw [if_pos (by rw [hpos0]; simpa using hend)]
      rw [hnone] at hsome
      simp at hsome
    · have hlt : st.pos < input.bytes.size := Nat.lt_of_le_of_ne hpos hend
      have hbody := boundaryBody_depth p atoms input _ (st.ci % p.ring) prev
        (liveAt st (st.ci % p.ring) (paGen st.ci)) d seed depth hwf hawf hring hnl ha hinv0 rfl
        (by rw [hself]; rfl) (by rw [hself]; rfl) hfresh0 hstamps0 hgens0 (by rw [hci0, hpos0]; exact hd.ciPos)
        (by rw [hpos0]; exact hlt)
      obtain ⟨hidleBody, hcont⟩ := hbody
      obtain ⟨hci', hpos', hstamps', hgens', hlive'⟩ := hcont hsome
      rw [hci0] at hci' hstamps' hgens' hlive'
      rw [hpos0] at hpos'
      -- The boundary past the depth cannot continue: it is idle, so the run stops there.
      have hci1 : st.ci ≤ depth := by
        rcases hcile with h | h
        · by_cases hle : st.ci ≤ depth
          · exact hle
          · exfalso
            have hnil := hidle (by omega)
            -- The filter did not jump, so a match is known.
            have hmatched : st.matched = true := by
              by_cases hm : st.matched = true
              · exact hm
              · exfalso
                apply hcond
                simp only [Bool.and_eq_true, List.isEmpty_iff, Bool.not_eq_true', decide_eq_true_eq, hpos0,
                  St.matched_filterSlot, St.matched_bumpBoundaries]
                refine ⟨⟨⟨⟨hnil, by simpa using hm⟩, ha.enabled⟩, hlt⟩, ?_⟩
                exact bolAt_false input st.pos prev hnl (by have := hd.ciPos; omega)
            have hnone := hidleBody hnil (by simpa using hmatched)
            rw [hnone] at hsome
            simp at hsome
        · exact absurd h hend
      refine ⟨⟨?_, ?_, ?_, ?_, ?_, ?_⟩, hci'⟩
      · intro sj hsj pc hpc; rw [hci']; exact hstamps' sj hsj pc hpc
      · intro sj hsj; rw [hci']; exact hgens' sj hsj
      · intro pc hpc hf; rw [hci'] at hf ⊢; exact hlive' pc hpc hf
      · rw [hci']; have := hd.ciPos; omega
      · rw [hci']; omega
      · intro h; rw [hci'] at h; omega
/-! ## The run visits at most `depth + 3` boundaries -/

theorem prepare_depthInv (p : Prog) (d : Nat → Nat) (depth : Nat) (input : Input) (hring : 1 ≤ p.ring) :
    DepthInv p d depth input (prepare p) := by
  have hslot : ∀ si, si < p.ring → ∀ pc, ((prepare p).slot si).entry pc = if pc < p.n then
      { ctr := List.replicate p.k 0 } else default := by
    intro si hsi pc
    rw [prepare_slot p si hsi]
    simp only [Slot.entry]
    split
    · rename_i h; simp [h]
    · rename_i h; simp [h]
  refine ⟨?_, ?_, ?_, by simp [prepare], by simp [prepare], by simp [prepare]⟩
  · intro si hsi pc hpc
    rw [hslot si hsi pc, if_pos hpc]
    simp [prepare, paGen]
  · intro si hsi
    rw [prepare_slot p si hsi]
    simp [prepare, paGen]
  · intro pc hpc hf
    have hsi : (prepare p).ci % p.ring < p.ring := Nat.mod_lt _ (by omega)
    rw [hslot _ hsi pc, if_pos hpc] at hf
    simp [prepare, paGen] at hf

theorem paRun_depth (p : Prog) (atoms : Atoms) (input : Input) (atom : Nat) (d : Nat → Nat) (seed : Nat → Bool)
    (depth : Nat) (hwf : p.wf) (hawf : atoms.wf p) (hring : p.ring = 2) (hnl : input.nlMode = false)
    (ha : Anchored p d seed depth) :
    ∀ (fuel : Nat) (st : St) (prev : Int), GlobalInv p st → DepthInv p d depth input st →
      st.pos ≤ input.bytes.size →
      stepFigure (paRun p atoms input st prev fuel).m p.k p.ring atom ≤
        stepFigure st.m p.k p.ring atom + (depth + 3 - st.ci) * perBoundary p atom +
          (input.bytes.size - st.pos) := by
  have hring2 : 2 ≤ p.ring := by omega
  intro fuel
  induction fuel with
  | zero => intro st prev _ _ _; simp only [paRun]; omega
  | succ fuel ih =>
    intro st prev hinv hd hpos
    obtain ⟨h1, h2, h3, h4, h5⟩ := boundaryStep_step p atoms input st prev atom hwf hawf hring2 hinv hpos
    have hdep := boundaryStep_depth p atoms input st prev d seed depth hwf hawf hring hnl ha hinv hd hpos
    simp only [paRun]
    split
    · rename_i st' hst
      have hp : (boundaryStep p atoms input st prev).1 = st' := by rw [hst]
      rw [hp] at h2 h3 h4
      have hci := hd.ciDepth
      have hone : 1 ≤ depth + 3 - st.ci := by omega
      have hmul : perBoundary p atom ≤ (depth + 3 - st.ci) * perBoundary p atom :=
        Nat.le_mul_of_pos_left _ hone
      omega
    · rename_i st' prev' hst
      have hp : (boundaryStep p atoms input st prev).1 = st' := by rw [hst]
      have hsome : (boundaryStep p atoms input st prev).2.isSome := by rw [hst]; rfl
      obtain ⟨hd', hci'⟩ := hdep hsome
      have hlt := h5 hsome
      rw [hp] at h1 h2 h3 h4 hlt hd' hci'
      have hc := ih st' prev' h1 hd' h4
      have hcile : st.ci + 1 ≤ depth + 2 := by have := hd'.ciDepth; omega
      have heq : depth + 3 - st.ci = (depth + 3 - st'.ci) + 1 := by omega
      rw [heq, Nat.succ_mul]
      omega

/-- The run of a bounded program pays for `depth + 3` boundaries. -/
theorem run_steps_le_depth (p : Prog) (atoms : Atoms) (input : Input) (atom : Nat) (d : Nat → Nat)
    (seed : Nat → Bool) (depth : Nat) (hwf : p.wf) (hawf : atoms.wf p) (hring : p.ring = 2)
    (hnl : input.nlMode = false) (ha : Anchored p d seed depth) :
    stepFigure (run p atoms input).m p.k p.ring atom ≤
      24 + p.ring + input.bytes.size + (depth + 3) * perBoundary p atom := by
  have hrun : (run p atoms input).m = (paRun p atoms input (prepare p) (-2) (input.bytes.size + 2)).m := rfl
  rw [hrun]
  have hc := paRun_depth p atoms input atom d seed depth hwf hawf hring hnl ha (input.bytes.size + 2) (prepare p) (-2)
    (prepare_globalInv p) (prepare_depthInv p d depth input (by omega)) (by simp [prepare])
  rw [prepare_stepFigure] at hc
  have hp0 : (prepare p).pos = 0 := rfl
  have hc0 : (prepare p).ci = 0 := rfl
  rw [hp0, hc0] at hc
  simp only [Nat.sub_zero] at hc
  omega

/--
Phase A never exceeds the anchored step figure: for every program with the certificate, every atom test and
every subject without newline mode.
-/
theorem run_steps_le_anchored (p : Prog) (atoms : Atoms) (input : Input) (atom : Nat) (d : Nat → Nat)
    (seed : Nat → Bool) (depth : Nat) (hwf : p.wf) (hawf : atoms.wf p) (hring : p.ring = 2)
    (hnl : input.nlMode = false) (ha : Anchored p d seed depth) :
    stepFigure (run p atoms input).m p.k p.ring atom ≤ stepsFigureAnchored p atom input.bytes.size depth := by
  unfold stepsFigureAnchored
  have h1 := run_steps_le p atoms input atom hwf hawf (by omega)
  have h2 := run_steps_le_depth p atoms input atom d seed depth hwf hawf hring hnl ha
  unfold stepsFigure at h1
  by_cases hle : input.bytes.size + 1 ≤ depth + 3
  · rw [Nat.min_eq_left hle]; exact h1
  · rw [Nat.min_eq_right (by omega)]; exact h2

end PhaseA
end Vego
