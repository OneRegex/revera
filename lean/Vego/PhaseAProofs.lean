/-
Universal bounds on the phase A model: for every well-formed program, every atom test, and every subject,
the workspace allocations stay within the heap figure and the events stay within the step figure.

The two hard parts are the closure and the buffers.
The closure is bounded by a potential: the queue length plus, for every instruction of the current slot,
the rank of its stored payload among the payloads that exist at that boundary. A successful merge stores
a strictly better payload, so the rank drops, and every pop shortens the queue. The potential pays for
every pop, every relaxation and every compaction.
The buffers are bounded through the growth rule: the active list of a slot holds distinct instructions,
the queue never passes twice the program length plus one, and the geometric argument of `CostLemmas.lean`
turns those needs into capacities and bytes.
-/

import Vego.PhaseA

namespace Vego
namespace PhaseA

/-! ## The strict order on payloads -/

theorem ctrLess_irrefl (c : List Nat) : ctrLess c c = false := by
  induction c with
  | nil => rfl
  | cons a as ih => simp [ctrLess, ih]

theorem ctrLess_trans (a b c : List Nat) (hab : ctrLess a b = true) (hbc : ctrLess b c = true) :
    ctrLess a c = true := by
  induction a generalizing b c with
  | nil => simp [ctrLess] at hab
  | cons x xs ih =>
    cases b with
    | nil => simp [ctrLess] at hab
    | cons y ys =>
      cases c with
      | nil => simp [ctrLess] at hbc
      | cons z zs =>
        simp only [ctrLess, bne_iff_ne, ne_eq, ite_not] at hab hbc ⊢
        split at hab
        · rename_i hxy
          subst hxy
          split at hbc
          · rename_i hyz; subst hyz; simp; exact ih ys zs hab hbc
          · rename_i hyz
            simp only [decide_eq_true_eq] at hbc
            rw [if_neg hyz]; simpa using hbc
        · rename_i hxy
          simp only [decide_eq_true_eq] at hab
          split at hbc
          · rename_i hyz; subst hyz; rw [if_neg hxy]; simpa using hab
          · rename_i hyz
            simp only [decide_eq_true_eq] at hbc
            have : x ≠ z := by omega
            rw [if_neg this]; simp; omega

/-- A payload: the match start and the counter vector. -/
abbrev Payload := Nat × List Nat

/-- The strict order `paStore` merges by: an earlier start, or the same start and smaller counters. -/
def plt (k : Nat) (a b : Payload) : Bool :=
  a.1 < b.1 || (a.1 == b.1 && k != 0 && ctrLess a.2 b.2)

theorem plt_irrefl (k : Nat) (a : Payload) : plt k a a = false := by
  simp [plt, ctrLess_irrefl]

theorem plt_trans (k : Nat) (a b c : Payload) (hab : plt k a b = true) (hbc : plt k b c = true) :
    plt k a c = true := by
  simp only [plt, Bool.or_eq_true, Bool.and_eq_true, decide_eq_true_eq, beq_iff_eq, bne_iff_ne,
    ne_eq] at *
  rcases hab with h1 | h1 <;> rcases hbc with h2 | h2
  · left; omega
  · left; have := h2.1.1; omega
  · left; have := h1.1.1; omega
  · right
    refine ⟨⟨?_, h1.1.2⟩, ctrLess_trans _ _ _ h1.2 h2.2⟩
    have := h1.1.1
    have := h2.1.1
    omega

theorem betterThan_eq_plt (k : Nat) (start : Nat) (ctr : List Nat) (e : Entry) :
    betterThan k start ctr e = plt k (start, ctr) (e.start, e.ctr) := by
  simp [betterThan, plt]

/-! ## Rank: how many known payloads lie strictly below a stored one -/

/-- The rank of a payload among the known payloads `V`. -/
def rankOf (k : Nat) (V : List Payload) (v : Payload) : Nat :=
  V.countP fun w => plt k w v

theorem rankOf_le (k : Nat) (V : List Payload) (v : Payload) : rankOf k V v ≤ V.length :=
  List.countP_le_length

/-- A known payload strictly below another has a strictly smaller rank. -/
theorem rankOf_lt (k : Nat) (V : List Payload) (v old : Payload) (hv : v ∈ V)
    (hlt : plt k v old = true) : rankOf k V v < rankOf k V old := by
  unfold rankOf
  induction V with
  | nil => simp at hv
  | cons w ws ih =>
    simp only [List.countP_cons]
    have hmono : List.countP (fun x => plt k x v) ws ≤ List.countP (fun x => plt k x old) ws :=
      List.countP_mono_left fun x _ hx => plt_trans k x v old hx hlt
    rcases List.mem_cons.mp hv with hwv | hwv
    · subst hwv
      simp [plt_irrefl, hlt]
      omega
    · have := ih hwv
      by_cases hw : plt k w v = true
      · have : plt k w old = true := plt_trans k w v old hw hlt
        simp [hw, this]
        omega
      · rw [if_neg hw]
        split <;> omega

/-- A known payload is not below itself, so its rank is below the number of known payloads. -/
theorem rankOf_lt_length (k : Nat) (V : List Payload) (v : Payload) (hv : v ∈ V) :
    rankOf k V v < V.length := by
  unfold rankOf
  have h := List.length_eq_countP_add_countP (fun w => plt k w v) (l := V)
  have hpos : 0 < List.countP (fun w => decide ¬(plt k w v = true)) V :=
    List.countP_pos_iff.mpr ⟨v, hv, by simp [plt_irrefl]⟩
  omega

/-! ## Distinct instructions below the program length -/

theorem nodup_length_le (l : List Nat) (n : Nat) (hnd : l.Nodup) (hlt : ∀ x ∈ l, x < n) :
    l.length ≤ n := by
  induction n generalizing l with
  | zero =>
    cases l with
    | nil => simp
    | cons x xs => exact absurd (hlt x (by simp)) (by omega)
  | succ n ih =>
    have h1 := List.length_eq_countP_add_countP (fun x => x == n) (l := l)
    have h2 : List.countP (fun x => x == n) l ≤ 1 := by
      have := (List.nodup_iff_count.mp hnd) n
      rwa [List.count_eq_countP] at this
    have h3 : List.countP (fun x => decide ¬((x == n) = true)) l ≤ n := by
      have := ih (l.filter fun x => decide ¬((x == n) = true)) (List.Pairwise.filter _ hnd) (by
        intro x hx
        rw [List.mem_filter] at hx
        have := hlt x hx.1
        have hne : x ≠ n := by simpa using hx.2
        omega)
      rwa [List.countP_eq_length_filter]
    omega

theorem mem_dedupFirstN (fuel : Nat) (l : List Nat) (hf : l.length ≤ fuel) (x : Nat) :
    x ∈ dedupFirstN fuel l ↔ x ∈ l := by
  induction fuel generalizing l with
  | zero =>
    cases l with
    | nil => simp [dedupFirstN]
    | cons y ys => simp at hf
  | succ fuel ih =>
    cases l with
    | nil => simp [dedupFirstN]
    | cons y ys =>
      simp only [dedupFirstN, List.mem_cons]
      have hlen : (ys.filter (· != y)).length ≤ fuel :=
        Nat.le_trans (List.length_filter_le _ _) (by simpa using hf)
      rw [ih _ hlen]
      simp only [List.mem_filter, bne_iff_ne, ne_eq]
      constructor
      · rintro (h | ⟨h, _⟩) <;> simp [h]
      · rintro (h | h)
        · exact Or.inl h
        · by_cases hxy : x = y
          · exact Or.inl hxy
          · exact Or.inr ⟨h, hxy⟩

theorem dedupFirstN_nodup (fuel : Nat) (l : List Nat) (hf : l.length ≤ fuel) :
    (dedupFirstN fuel l).Nodup := by
  induction fuel generalizing l with
  | zero =>
    cases l with
    | nil => simp [dedupFirstN]
    | cons y ys => simp at hf
  | succ fuel ih =>
    cases l with
    | nil => simp [dedupFirstN]
    | cons y ys =>
      simp only [dedupFirstN]
      have hlen : (ys.filter (· != y)).length ≤ fuel :=
        Nat.le_trans (List.length_filter_le _ _) (by simpa using hf)
      rw [List.nodup_cons]
      refine ⟨?_, ih _ hlen⟩
      intro hmem
      rw [mem_dedupFirstN _ _ hlen] at hmem
      simp at hmem

theorem mem_dedupFirst (l : List Nat) (x : Nat) : x ∈ dedupFirst l ↔ x ∈ l :=
  mem_dedupFirstN _ _ (Nat.le_refl _) x

theorem dedupFirst_nodup (l : List Nat) : (dedupFirst l).Nodup :=
  dedupFirstN_nodup _ _ (Nat.le_refl _)

/-- The compacted queue holds at most one entry per instruction. -/
theorem dedupFirst_length_le (l : List Nat) (n : Nat) (hlt : ∀ x ∈ l, x < n) :
    (dedupFirst l).length ≤ n :=
  nodup_length_le _ _ (dedupFirst_nodup l) fun x hx => hlt x ((mem_dedupFirst l x).mp hx)

/-! ## Slots and states -/

theorem St.slot_setSlot_self (st : St) (si : Nat) (sl : Slot) (h : si < st.slots.size) :
    (st.setSlot si sl).slot si = sl := by
  simp [St.setSlot, St.slot, h]

theorem St.slot_setSlot_ne (st : St) (si sj : Nat) (sl : Slot) (h : si ≠ sj) :
    (st.setSlot si sl).slot sj = st.slot sj := by
  simp [St.setSlot, St.slot, h]

@[simp] theorem St.slots_size_setSlot (st : St) (si : Nat) (sl : Slot) :
    (st.setSlot si sl).slots.size = st.slots.size := by
  simp [St.setSlot]

@[simp] theorem St.queue_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).queue = st.queue := rfl
@[simp] theorem St.queueCap_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).queueCap = st.queueCap := rfl
@[simp] theorem St.aheadCap_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).aheadCap = st.aheadCap := rfl
@[simp] theorem St.ahead_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).ahead = st.ahead := rfl
@[simp] theorem St.queueCap_charge (st : St) (b : Nat) : (st.charge b).queueCap = st.queueCap := rfl
@[simp] theorem St.aheadCap_charge (st : St) (b : Nat) : (st.charge b).aheadCap = st.aheadCap := rfl
@[simp] theorem St.ahead_charge (st : St) (b : Nat) : (st.charge b).ahead = st.ahead := rfl
@[simp] theorem St.allocBytes_charge (st : St) (b : Nat) : (st.charge b).m.allocBytes = st.m.allocBytes + b := rfl
@[simp] theorem St.m_setSlot (st : St) (si : Nat) (sl : Slot) : (st.setSlot si sl).m = st.m := rfl
@[simp] theorem St.slots_charge (st : St) (b : Nat) : (st.charge b).slots = st.slots := rfl
@[simp] theorem St.queue_charge (st : St) (b : Nat) : (st.charge b).queue = st.queue := rfl
@[simp] theorem St.slot_charge (st : St) (b si : Nat) : (st.charge b).slot si = st.slot si := rfl

/-- An entry depends on the table alone. -/
theorem Slot.entry_eq (sl : Slot) (pc : Nat) : sl.entry pc = sl.table[pc]?.getD default := rfl

theorem entry_set_self (tbl : Array Entry) (pc : Nat) (e : Entry) (h : pc < tbl.size) :
    (tbl.setIfInBounds pc e)[pc]?.getD default = e := by
  simp [h]

theorem entry_set_ne (tbl : Array Entry) (pc q : Nat) (e : Entry) (h : pc ≠ q) :
    (tbl.setIfInBounds pc e)[q]?.getD default = tbl[q]?.getD default := by
  simp [h]

/-! ## Storing a payload -/

theorem storeInto_gen (sl : Slot) (pc start : Nat) (ctr : List Nat) :
    (storeInto sl pc start ctr).1.gen = sl.gen := by
  unfold storeInto; split <;> rfl

theorem storeInto_size (sl : Slot) (pc start : Nat) (ctr : List Nat) :
    (storeInto sl pc start ctr).1.table.size = sl.table.size := by
  unfold storeInto; split <;> simp

theorem storeInto_entry_self (sl : Slot) (pc start : Nat) (ctr : List Nat) (h : pc < sl.table.size) :
    (storeInto sl pc start ctr).1.entry pc = { stamp := sl.gen, start, ctr } := by
  unfold storeInto
  split
  · rename_i hs
    simp only [beq_iff_eq] at hs
    simp only [Slot.entry_eq, entry_set_self _ _ _ h]
    rw [Slot.entry_eq] at hs
    rw [hs]
  · simp only [Slot.entry_eq, entry_set_self _ _ _ h]

theorem storeInto_entry_ne (sl : Slot) (pc q start : Nat) (ctr : List Nat) (h : pc ≠ q) :
    (storeInto sl pc start ctr).1.entry q = sl.entry q := by
  unfold storeInto
  split <;> simp only [Slot.entry_eq, entry_set_ne _ _ _ _ h]

/-- The active list after a store: unchanged for a fresh entry, extended by `pc` for a stale one. -/
theorem storeInto_active (sl : Slot) (pc start : Nat) (ctr : List Nat) :
    (storeInto sl pc start ctr).1.active =
      if (sl.entry pc).stamp == sl.gen then sl.active else sl.active ++ [pc] := by
  unfold storeInto; split <;> simp_all

/-! ## Rank sums over the instructions of a slot -/

/-- The payloads known at a boundary: the spawn, and the payload of every fresh entry of the slot. -/
def knownPayloads (spawn : Payload) (sl : Slot) : List Payload :=
  spawn :: (sl.table.toList.filterMap fun e =>
    if e.stamp == sl.gen then some (e.start, e.ctr) else none)

theorem knownPayloads_length (spawn : Payload) (sl : Slot) :
    (knownPayloads spawn sl).length ≤ sl.table.size + 1 := by
  simp only [knownPayloads, List.length_cons]
  have := List.length_filterMap_le (fun e : Entry =>
    if e.stamp == sl.gen then some (e.start, e.ctr) else none) sl.table.toList
  have hl : sl.table.toList.length = sl.table.size := Array.length_toList
  omega

theorem spawn_mem_knownPayloads (spawn : Payload) (sl : Slot) : spawn ∈ knownPayloads spawn sl := by
  simp [knownPayloads]

/-- Every fresh entry of the slot holds a known payload. -/
theorem fresh_mem_knownPayloads (spawn : Payload) (sl : Slot) (pc : Nat) (h : pc < sl.table.size)
    (hf : (sl.entry pc).stamp = sl.gen) : ((sl.entry pc).start, (sl.entry pc).ctr) ∈ knownPayloads spawn sl := by
  simp only [knownPayloads, List.mem_cons, List.mem_filterMap]
  right
  refine ⟨sl.entry pc, ?_, by simp [hf]⟩
  simp only [Slot.entry, Array.getElem?_eq_getElem h, Option.getD_some]
  exact Array.getElem_mem_toList h

/-- The rank of the payload stored at `pc`, or the number of known payloads when nothing is stored. -/
def rankAt (k : Nat) (V : List Payload) (sl : Slot) (pc : Nat) : Nat :=
  if (sl.entry pc).stamp = sl.gen then rankOf k V ((sl.entry pc).start, (sl.entry pc).ctr) else V.length

theorem rankAt_le (k : Nat) (V : List Payload) (sl : Slot) (pc : Nat) : rankAt k V sl pc ≤ V.length := by
  unfold rankAt; split
  · exact rankOf_le _ _ _
  · exact Nat.le_refl _

def rankSum (k : Nat) (V : List Payload) (sl : Slot) (n : Nat) : Nat :=
  ((List.range n).map (rankAt k V sl)).sum

theorem rankSum_le (k : Nat) (V : List Payload) (sl : Slot) (n : Nat) : rankSum k V sl n ≤ n * V.length := by
  unfold rankSum
  induction n with
  | zero => simp
  | succ n ih =>
    rw [List.range_succ, List.map_append, List.sum_append]
    simp only [List.map_cons, List.map_nil, List.sum_cons, List.sum_nil, Nat.add_zero]
    have := rankAt_le k V sl n
    rw [Nat.succ_mul]
    omega

theorem sum_map_le (f g : Nat → Nat) (l : List Nat) (h : ∀ x ∈ l, g x ≤ f x) :
    (l.map g).sum ≤ (l.map f).sum := by
  induction l with
  | nil => simp
  | cons x xs ih =>
    simp only [List.map_cons, List.sum_cons]
    have := h x (by simp)
    have := ih fun y hy => h y (by simp [hy])
    omega

/-- A sum over the range drops by at least one when one term drops by at least one and the rest hold. -/
theorem sum_range_drop (f g : Nat → Nat) (n pc : Nat) (hpc : pc < n) (hdrop : g pc + 1 ≤ f pc)
    (hrest : ∀ q, q ≠ pc → g q = f q) :
    ((List.range n).map g).sum + 1 ≤ ((List.range n).map f).sum := by
  induction n with
  | zero => omega
  | succ n ih =>
    rw [List.range_succ, List.map_append, List.sum_append, List.map_append, List.sum_append]
    simp only [List.map_cons, List.map_nil, List.sum_cons, List.sum_nil, Nat.add_zero]
    by_cases h : pc = n
    · subst h
      have hle : ((List.range pc).map g).sum ≤ ((List.range pc).map f).sum := by
        apply sum_map_le
        intro q hq
        rw [List.mem_range] at hq
        rw [hrest q (by omega)]
        exact Nat.le_refl _
      omega
    · have := ih (by omega)
      rw [hrest n (Ne.symm h)]
      omega

/-- Storing a known payload that the slot accepts lowers the rank sum. -/
theorem rankSum_storeInto (k : Nat) (V : List Payload) (sl : Slot) (pc start : Nat) (ctr : List Nat)
    (hpc : pc < sl.table.size) (hV : (start, ctr) ∈ V) (hacc : acceptsStore k sl pc start ctr = true) :
    rankSum k V (storeInto sl pc start ctr).1 sl.table.size + 1 ≤ rankSum k V sl sl.table.size := by
  unfold rankSum
  apply sum_range_drop _ _ _ pc hpc
  · unfold rankAt
    rw [storeInto_entry_self _ _ _ _ hpc, storeInto_gen]
    simp only [ite_true]
    unfold acceptsStore at hacc
    simp only [Bool.or_eq_true, bne_iff_ne, ne_eq] at hacc
    split
    · rename_i hf
      rcases hacc with h | h
      · exact absurd hf h
      · rw [betterThan_eq_plt] at h
        exact rankOf_lt k V _ _ hV h
    · exact rankOf_lt_length k V _ hV
  · intro q hq
    unfold rankAt
    rw [storeInto_entry_ne _ _ _ _ _ (Ne.symm hq), storeInto_gen]

/-- Fresh entries keep holding known payloads through a store of a known payload. -/
theorem freshKnown_storeInto (V : List Payload) (sl : Slot) (pc start : Nat) (ctr : List Nat)
    (hV : (start, ctr) ∈ V)
    (hfresh : ∀ q, q < sl.table.size → (sl.entry q).stamp = sl.gen → ((sl.entry q).start, (sl.entry q).ctr) ∈ V) :
    ∀ q, q < (storeInto sl pc start ctr).1.table.size →
      ((storeInto sl pc start ctr).1.entry q).stamp = (storeInto sl pc start ctr).1.gen →
      (((storeInto sl pc start ctr).1.entry q).start, ((storeInto sl pc start ctr).1.entry q).ctr) ∈ V := by
  intro q hq hf
  rw [storeInto_size] at hq
  rw [storeInto_gen] at hf
  by_cases h : pc = q
  · subst h
    rw [storeInto_entry_self _ _ _ _ hq] at hf ⊢
    exact hV
  · rw [storeInto_entry_ne _ _ _ _ _ h] at hf ⊢
    exact hfresh q hq hf

/-! ## The closure potential -/

/-- Well-formed programs: every jump target and the start instruction are inside the program. -/
def Prog.wf (p : Prog) : Prop :=
  p.start < p.n ∧ ∀ pc, pc < p.n → (p.ins.getD pc default).next < p.n ∧ (p.ins.getD pc default).alt < p.n

/-- The invariant of one closure at slot `si` with known payloads `V`. -/
structure ClosureInv (p : Prog) (V : List Payload) (si : Nat) (st : St) : Prop where
  slotsSize : st.slots.size = p.ring
  siLt : si < p.ring
  tableSize : (st.slot si).table.size = p.n
  queueLt : ∀ pc ∈ st.queue, pc < p.n
  fresh : ∀ q, q < p.n → ((st.slot si).entry q).stamp = (st.slot si).gen →
    (((st.slot si).entry q).start, ((st.slot si).entry q).ctr) ∈ V

/-- The closure's share of the step figure. -/
def ccost (k : Nat) (m : Meter) : Nat :=
  m.pops + m.compactWork + m.relaxes * (2 * k + 7) + m.considers * (2 * k + 5)

/-- The measure: the queue length plus the rank sum of the slot. -/
def measure (p : Prog) (V : List Payload) (si : Nat) (st : St) : Nat :=
  st.queue.length + rankSum p.k V (st.slot si) p.n

@[simp] theorem St.slot_mk_m (st : St) (m : Meter) (si : Nat) :
    ({ st with m := m } : St).slot si = st.slot si := rfl
@[simp] theorem St.slots_mk_m (st : St) (m : Meter) : ({ st with m := m } : St).slots = st.slots := rfl
@[simp] theorem St.queue_mk_m (st : St) (m : Meter) : ({ st with m := m } : St).queue = st.queue := rfl

@[simp] theorem St.slot_pushQueue (st : St) (pc si : Nat) : (st.pushQueue pc).slot si = st.slot si := rfl
@[simp] theorem St.slots_pushQueue (st : St) (pc : Nat) : (st.pushQueue pc).slots = st.slots := rfl
@[simp] theorem St.queue_pushQueue (st : St) (pc : Nat) : (st.pushQueue pc).queue = pc :: st.queue := rfl
@[simp] theorem St.pops_pushQueue (st : St) (pc : Nat) : (st.pushQueue pc).m.pops = st.m.pops := rfl
@[simp] theorem St.compactWork_pushQueue (st : St) (pc : Nat) :
    (st.pushQueue pc).m.compactWork = st.m.compactWork := rfl
@[simp] theorem St.relaxes_pushQueue (st : St) (pc : Nat) : (st.pushQueue pc).m.relaxes = st.m.relaxes := rfl
@[simp] theorem St.considers_pushQueue (st : St) (pc : Nat) :
    (st.pushQueue pc).m.considers = st.m.considers := rfl

theorem considerCore_queue (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (considerCore k st start ctr e).queue = st.queue := by
  unfold considerCore; split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl

theorem considerCore_slots (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (considerCore k st start ctr e).slots = st.slots := by
  unfold considerCore; split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl

theorem considerCore_m (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (considerCore k st start ctr e).m = st.m := by
  unfold considerCore; split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl

@[simp] theorem paConsider_queue (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (paConsider k st start ctr e).queue = st.queue := by
  simp [paConsider, considerCore_queue]

@[simp] theorem paConsider_slots (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (paConsider k st start ctr e).slots = st.slots := by
  simp [paConsider, considerCore_slots]

@[simp] theorem paConsider_slot (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e si : Nat) :
    (paConsider k st start ctr e).slot si = st.slot si := by
  simp [St.slot, paConsider_slots]

@[simp] theorem paConsider_m (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (paConsider k st start ctr e).m = { st.m with considers := st.m.considers + 1 } := by
  simp [paConsider, considerCore_m]

/-- What `paStore` does to the state: nothing when the payload is rejected, one store otherwise. -/
theorem paStore_cases (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    (paStore k st si pc start ctr = (false, st)) ∨
    (paStore k st si pc start ctr =
      (true, (st.setSlot si (storeInto (st.slot si) pc start ctr).1).charge
        (storeInto (st.slot si) pc start ctr).2) ∧
     acceptsStore k (st.slot si) pc start ctr = true) := by
  unfold paStore
  split
  · left; rfl
  · split
    · right; exact ⟨rfl, by assumption⟩
    · left; rfl

theorem ClosureInv.slot_lt (h : ClosureInv p V si st) : si < st.slots.size := by
  rw [h.slotsSize]; exact h.siLt

/-- The invariant only looks at the slots and the queue, so a meter change keeps it. -/
theorem ClosureInv.mk_m (h : ClosureInv p V si st) (m : Meter) : ClosureInv p V si { st with m := m } :=
  ⟨h.slotsSize, h.siLt, h.tableSize, h.queueLt, h.fresh⟩

theorem ClosureInv.pushQueue (h : ClosureInv p V si st) (pc : Nat) (hpc : pc < p.n) :
    ClosureInv p V si (st.pushQueue pc) :=
  ⟨by simpa using h.slotsSize, h.siLt, by simpa using h.tableSize,
   fun q hq => by
     simp only [St.queue_pushQueue, List.mem_cons] at hq
     rcases hq with hq | hq
     · subst hq; exact hpc
     · exact h.queueLt q hq,
   fun q hq hf => by simpa using h.fresh q hq (by simpa using hf)⟩

/-- A store of a known payload at an instruction inside the program keeps the invariant and lowers the rank sum. -/
theorem store_step (p : Prog) (V : List Payload) (si : Nat) (st : St) (pc start : Nat) (ctr : List Nat)
    (hinv : ClosureInv p V si st) (hpc : pc < p.n) (hV : (start, ctr) ∈ V)
    (hacc : acceptsStore p.k (st.slot si) pc start ctr = true) :
    ClosureInv p V si ((st.setSlot si (storeInto (st.slot si) pc start ctr).1).charge
      (storeInto (st.slot si) pc start ctr).2) ∧
    rankSum p.k V (((st.setSlot si (storeInto (st.slot si) pc start ctr).1).charge
      (storeInto (st.slot si) pc start ctr).2).slot si) p.n + 1 ≤ rankSum p.k V (st.slot si) p.n := by
  have hslot : ((st.setSlot si (storeInto (st.slot si) pc start ctr).1).charge
      (storeInto (st.slot si) pc start ctr).2).slot si = (storeInto (st.slot si) pc start ctr).1 := by
    simp only [St.slot_charge]
    exact St.slot_setSlot_self _ _ _ hinv.slot_lt
  have hsize := hinv.tableSize
  refine ⟨?_, ?_⟩
  · refine ⟨by simp [hinv.slotsSize], hinv.siLt, ?_, hinv.queueLt, ?_⟩
    · rw [hslot, storeInto_size, hsize]
    · rw [hslot]
      have := freshKnown_storeInto V (st.slot si) pc start ctr hV (fun q hq => hinv.fresh q (hsize ▸ hq))
      intro q hq
      exact this q (by rw [storeInto_size, hsize]; exact hq)
  · rw [hslot]
    have := rankSum_storeInto p.k V (st.slot si) pc start ctr (hsize ▸ hpc) hV hacc
    rwa [hsize] at this

/-- One relaxation: the measure never rises, and the closure cost rises by one relaxation. -/
theorem paRelax_step (p : Prog) (V : List Payload) (si : Nat) (st : St) (pc start : Nat) (ctr : List Nat)
    (hinv : ClosureInv p V si st) (hpc : pc < p.n) (hV : (start, ctr) ∈ V) :
    ClosureInv p V si (paRelax p.k st si pc start ctr) ∧
    measure p V si (paRelax p.k st si pc start ctr) ≤ measure p V si st ∧
    ccost p.k (paRelax p.k st si pc start ctr).m = ccost p.k st.m + (2 * p.k + 7) := by
  have hinv0 := hinv.mk_m { st.m with relaxes := st.m.relaxes + 1 }
  simp only [paRelax]
  rcases paStore_cases p.k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr with
    h | ⟨h, hacc⟩
  · rw [h]
    simp only [Bool.false_eq_true, ite_false]
    refine ⟨hinv0, Nat.le_refl _, ?_⟩
    simp only [ccost, Nat.add_mul, Nat.one_mul]
    omega
  · rw [h]
    simp only [ite_true]
    obtain ⟨hinv', hrank⟩ := store_step p V si _ pc start ctr hinv0 hpc hV hacc
    refine ⟨hinv'.pushQueue pc hpc, ?_, ?_⟩
    · simp only [measure, St.queue_pushQueue, List.length_cons, St.slot_pushQueue, St.queue_charge,
        St.queue_setSlot] at hrank ⊢
      rw [Nat.add_right_comm, Nat.add_assoc]
      exact Nat.add_le_add_left hrank _
    · simp only [ccost, St.pops_pushQueue, St.compactWork_pushQueue, St.relaxes_pushQueue,
        St.considers_pushQueue, St.charge, St.setSlot, Nat.add_mul, Nat.one_mul]
      omega

theorem weight_ge (k : Nat) : 22 ≤ weight k := by unfold weight; omega

theorem compactQueue_queue_len (st : St) (n : Nat) (h : ∀ pc ∈ st.queue, pc < n) :
    (compactQueue st).queue.length ≤ n := by
  simp only [compactQueue, List.length_reverse]
  apply dedupFirst_length_le
  intro x hx
  exact h x (List.mem_reverse.mp hx)

theorem compactQueue_mem (st : St) (pc : Nat) : pc ∈ (compactQueue st).queue ↔ pc ∈ st.queue := by
  simp [compactQueue, mem_dedupFirst]

/-- The arithmetic of one compaction: the potential released by the dropped entries pays for the scan. -/
theorem compact_arith (W L L' R n c : Nat) (hw : 22 ≤ W) (hL : 2 * n < L) (hL' : L' ≤ n) :
    c + 2 * L + 3 + W * (L' + R) ≤ c + W * (L + R) := by
  have hsplit : W * (L + R) = W * (L' + R) + W * (L - L') := by
    rw [← Nat.mul_add]
    congr 1
    omega
  have hgap : 22 * (L - L') ≤ W * (L - L') := Nat.mul_le_mul_right _ hw
  rw [hsplit]
  omega

/-- Compaction past twice the program length: the measure pays for the whole scan. -/
theorem compactQueue_step (p : Prog) (V : List Payload) (si : Nat) (st : St)
    (hinv : ClosureInv p V si st) (hlen : 2 * p.n < st.queue.length) :
    ClosureInv p V si (compactQueue st) ∧
    ccost p.k (compactQueue st).m + weight p.k * measure p V si (compactQueue st) ≤
      ccost p.k st.m + weight p.k * measure p V si st ∧
    measure p V si (compactQueue st) + 1 ≤ measure p V si st ∧
    (compactQueue st).queue ≠ [] := by
  have hlen' := compactQueue_queue_len st p.n hinv.queueLt
  have hslot : (compactQueue st).slot si = st.slot si := rfl
  refine ⟨⟨hinv.slotsSize, hinv.siLt, hinv.tableSize, fun pc hpc => hinv.queueLt pc ((compactQueue_mem st pc).mp hpc),
           hinv.fresh⟩, ?_, ?_, ?_⟩
  · have hw := weight_ge p.k
    have := compact_arith (weight p.k) st.queue.length (compactQueue st).queue.length
      (rankSum p.k V (st.slot si) p.n) p.n
      (st.m.pops + st.m.compactWork + st.m.relaxes * (2 * p.k + 7) + st.m.considers * (2 * p.k + 5))
      hw hlen hlen'
    simp only [ccost, measure, hslot]
    have hm : (compactQueue st).m = { st.m with compactWork := st.m.compactWork + 2 * st.queue.length + 3 } := rfl
    rw [hm]
    simp only []
    omega
  · simp only [measure, hslot]
    omega
  · intro hnil
    have : st.queue ≠ [] := by
      intro h; rw [h] at hlen; simp at hlen
    obtain ⟨x, hx⟩ := List.exists_mem_of_ne_nil st.queue this
    have := (compactQueue_mem st x).mpr hx
    rw [hnil] at this
    simp at this

/-- Popping the head of the queue. -/
theorem pop_inv (p : Prog) (V : List Payload) (si : Nat) (st : St) (pc : Nat) (rest : List Nat)
    (hinv : ClosureInv p V si st) (hq : st.queue = pc :: rest) :
    ClosureInv p V si (st.popQueue rest) :=
  ⟨hinv.slotsSize, hinv.siLt, hinv.tableSize,
   fun q hq' => hinv.queueLt q (by rw [hq]; exact List.mem_cons_of_mem _ hq'), hinv.fresh⟩

@[simp] theorem St.slot_popQueue (st : St) (rest : List Nat) (si : Nat) :
    (st.popQueue rest).slot si = st.slot si := rfl
@[simp] theorem St.queue_popQueue (st : St) (rest : List Nat) : (st.popQueue rest).queue = rest := rfl

theorem St.ccost_popQueue (k : Nat) (st : St) (rest : List Nat) :
    ccost k (st.popQueue rest).m = ccost k st.m + 1 := by
  simp only [ccost, St.popQueue]; omega

/-- Handling one fresh instruction: the invariant holds, the measure never rises, and the cost is bounded. -/
theorem handleOp_step (p : Prog) (V : List Payload) (si : Nat) (st : St) (pc start : Nat) (ctr : List Nat)
    (hwf : p.wf) (hinv : ClosureInv p V si st) (hpc : pc < p.n) (hV : (start, ctr) ∈ V) :
    ClosureInv p V si (handleOp p si st pc start ctr) ∧
    measure p V si (handleOp p si st pc start ctr) ≤ measure p V si st ∧
    ccost p.k (handleOp p si st pc start ctr).m ≤ ccost p.k st.m + (weight p.k - 1) := by
  have hnext := (hwf.2 pc hpc).1
  have halt := (hwf.2 pc hpc).2
  clear hpc
  unfold handleOp
  split
  · obtain ⟨hi1, hm1, hc1⟩ := paRelax_step p V si st _ _ _ hinv hnext hV
    obtain ⟨hi2, hm2, hc2⟩ := paRelax_step p V si _ _ _ _ hi1 halt hV
    refine ⟨hi2, Nat.le_trans hm2 hm1, ?_⟩
    rw [hc2, hc1]; unfold weight; omega
  · obtain ⟨hi1, hm1, hc1⟩ := paRelax_step p V si st _ _ _ hinv hnext hV
    refine ⟨hi1, hm1, ?_⟩
    rw [hc1]; unfold weight; omega
  · split
    · obtain ⟨hi1, hm1, hc1⟩ := paRelax_step p V si st _ _ _ hinv hnext hV
      refine ⟨hi1, hm1, ?_⟩
      rw [hc1]; unfold weight; omega
    · exact ⟨hinv, Nat.le_refl _, by omega⟩
  · split
    · obtain ⟨hi1, hm1, hc1⟩ := paRelax_step p V si st _ _ _ hinv hnext hV
      refine ⟨hi1, hm1, ?_⟩
      rw [hc1]; unfold weight; omega
    · exact ⟨hinv, Nat.le_refl _, by omega⟩
  · refine ⟨⟨by simpa using hinv.slotsSize, hinv.siLt, by simpa using hinv.tableSize,
        by simpa using hinv.queueLt, by simpa using hinv.fresh⟩, ?_, ?_⟩
    · simp only [measure, paConsider_queue, paConsider_slot]
      exact Nat.le_refl _
    · simp only [ccost, paConsider_m, Nat.add_mul, Nat.one_mul]
      unfold weight
      omega
  · exact ⟨hinv, Nat.le_refl _, by omega⟩

theorem handle_step (p : Prog) (V : List Payload) (si : Nat) (st : St) (pc : Nat)
    (hwf : p.wf) (hinv : ClosureInv p V si st) (hpc : pc < p.n) :
    ClosureInv p V si (handle p si st pc) ∧
    measure p V si (handle p si st pc) ≤ measure p V si st ∧
    ccost p.k (handle p si st pc).m ≤ ccost p.k st.m + (weight p.k - 1) := by
  unfold handle
  split
  · exact ⟨hinv, Nat.le_refl _, by omega⟩
  · rename_i hfresh
    simp only [bne_iff_ne, ne_eq, Decidable.not_not] at hfresh
    exact handleOp_step p V si st pc _ _ hwf hinv hpc (hinv.fresh pc hpc hfresh)

/-- One drain step: the invariant holds, the measure drops, and the weighted potential pays for the work. -/
theorem closureStep_step (p : Prog) (V : List Payload) (si : Nat) (st : St) (hwf : p.wf)
    (hinv : ClosureInv p V si st) (hne : st.queue ≠ []) :
    ClosureInv p V si (closureStep p st si) ∧
    ccost p.k (closureStep p st si).m + weight p.k * measure p V si (closureStep p st si) ≤
      ccost p.k st.m + weight p.k * measure p V si st ∧
    measure p V si (closureStep p st si) + 1 ≤ measure p V si st := by
  -- The optional compaction.
  have hstep1 : ∃ st1, (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = st1 ∧
      ClosureInv p V si st1 ∧
      ccost p.k st1.m + weight p.k * measure p V si st1 ≤ ccost p.k st.m + weight p.k * measure p V si st ∧
      measure p V si st1 ≤ measure p V si st ∧ st1.queue ≠ [] := by
    split
    · rename_i hc
      obtain ⟨h1, h2, h3, h4⟩ := compactQueue_step p V si st hinv (by simpa [queueCompactFactor] using hc)
      exact ⟨_, rfl, h1, h2, by omega, h4⟩
    · exact ⟨st, rfl, hinv, Nat.le_refl _, Nat.le_refl _, hne⟩
  obtain ⟨st1, hst1, hinv1, hcost1, hmeas1, hne1⟩ := hstep1
  unfold closureStep
  rw [hst1]
  obtain ⟨pc, rest, hq⟩ : ∃ pc rest, st1.queue = pc :: rest := by
    cases h : st1.queue with
    | nil => exact absurd h hne1
    | cons pc rest => exact ⟨pc, rest, rfl⟩
  unfold drain
  rw [hq]
  dsimp only
  have hinv2 := pop_inv p V si st1 pc rest hinv1 hq
  have hpc : pc < p.n := hinv1.queueLt pc (by rw [hq]; simp)
  have hmeas2 : measure p V si (st1.popQueue rest) + 1 = measure p V si st1 := by
    simp only [measure, hq, List.length_cons, St.slot_popQueue, St.queue_popQueue]
    omega
  have hcost2 := St.ccost_popQueue p.k st1 rest
  have hW := weight_ge p.k
  obtain ⟨hi3, hm3, hc3⟩ := handle_step p V si (st1.popQueue rest) pc hwf hinv2 hpc
  refine ⟨hi3, ?_, by omega⟩
  have : weight p.k * measure p V si (handle p si (st1.popQueue rest) pc) ≤
      weight p.k * measure p V si (st1.popQueue rest) := Nat.mul_le_mul_left _ hm3
  have h1 : weight p.k * measure p V si st1 = weight p.k * measure p V si (st1.popQueue rest) + weight p.k := by
    rw [← hmeas2, Nat.mul_succ]
  omega

/-- Draining the queue: the potential at entry bounds the whole closure cost, and enough fuel empties the queue. -/
theorem paClosure_bound (p : Prog) (V : List Payload) (si : Nat) (hwf : p.wf) :
    ∀ (fuel : Nat) (st : St), ClosureInv p V si st →
      ClosureInv p V si (paClosure p si st fuel) ∧
      ccost p.k (paClosure p si st fuel).m ≤ ccost p.k st.m + weight p.k * measure p V si st ∧
      (measure p V si st ≤ fuel → (paClosure p si st fuel).queue = []) := by
  intro fuel
  induction fuel with
  | zero =>
    intro st hinv
    refine ⟨hinv, by simp [paClosure], ?_⟩
    intro h
    simp only [paClosure]
    have : st.queue.length = 0 := by simp only [measure] at h; omega
    exact List.eq_nil_of_length_eq_zero this
  | succ fuel ih =>
    intro st hinv
    simp only [paClosure]
    split
    · rename_i hnil
      refine ⟨hinv, by omega, fun _ => ?_⟩
      exact List.isEmpty_iff.mp hnil
    · rename_i hnil
      have hne : st.queue ≠ [] := fun h => hnil (by simp [h])
      obtain ⟨hinv', hcost, hmeas⟩ := closureStep_step p V si st hwf hinv hne
      obtain ⟨hinvF, hcostF, hemptyF⟩ := ih (closureStep p st si) hinv'
      refine ⟨hinvF, by omega, fun h => hemptyF (by omega)⟩

/-! ## The global invariant: slots, queue, lookahead, and the allocation potential -/

/-- The invariant of one slot: distinct live instructions, each stamped with the slot's generation. -/
structure SlotInv (p : Prog) (sl : Slot) : Prop where
  size : sl.table.size = p.n
  nodup : sl.active.Nodup
  mem : ∀ pc ∈ sl.active, pc < p.n ∧ (sl.entry pc).stamp = sl.gen
  cap : sl.activeCap ≤ 2 * p.n + 8

theorem SlotInv.active_len (h : SlotInv p sl) : sl.active.length ≤ p.n :=
  nodup_length_le _ _ h.nodup fun pc hpc => (h.mem pc hpc).1

/-- The capacities the allocation potential sums: every active list, the queue, and the lookahead. -/
def capSum (p : Prog) (st : St) : Nat :=
  ((List.range p.ring).map fun si => (st.slot si).activeCap).sum + st.queueCap + st.aheadCap

structure GlobalInv (p : Prog) (st : St) : Prop where
  slotsSize : st.slots.size = p.ring
  slots : ∀ si, si < p.ring → SlotInv p (st.slot si)
  queueLt : ∀ pc ∈ st.queue, pc < p.n
  queueLen : st.queue.length ≤ 2 * p.n + 1
  queueCap : st.queueCap ≤ 4 * p.n + 26
  aheadLen : st.ahead.length ≤ maxElemAhead
  aheadCap : st.aheadCap ≤ 24
  alloc : st.m.allocBytes + 128 ≤ prepareBytes p.n p.k p.ring + 8 * capSum p st

/-- The growth rule keeps the potential ahead of the charge: a grown buffer at least doubles. -/
theorem grow_pot (len cap : Nat) : growBytes len cap + 8 * cap ≤ 8 * growCapAfter len cap := by
  unfold growBytes growCapAfter
  split
  · omega
  · have := growCap_doubles cap (len + 1)
    omega

theorem growCapAfter_le (len cap N : Nat) (hcap : cap ≤ 2 * N + 8) (hlen : len + 1 ≤ N) :
    growCapAfter len cap ≤ 2 * N + 8 := by
  unfold growCapAfter
  split
  · exact hcap
  · exact growCap_le cap (len + 1) N (by omega) hlen

/-- A queue capacity stays under its bound as long as the need does. -/
theorem growCapAfter_queue (len cap n : Nat) (hcap : cap ≤ 4 * n + 26) (hlen : len + 1 ≤ 2 * n + 1) :
    growCapAfter len cap ≤ 4 * n + 26 := by
  unfold growCapAfter
  split
  · exact hcap
  · have := growCap_le cap (len + 1) (2 * n + 1) (by omega) hlen
    omega

theorem growCapAfter_ahead (len cap : Nat) (hcap : cap ≤ 24) (hlen : len + 1 ≤ maxElemAhead) :
    growCapAfter len cap ≤ 24 := by
  unfold growCapAfter
  split
  · exact hcap
  · have := growCap_le cap (len + 1) maxElemAhead (by omega) hlen
    unfold maxElemAhead at this
    omega

/-- Replacing one term of a range sum. -/
theorem sum_range_update (f g : Nat → Nat) (n pc : Nat) (hpc : pc < n) (hrest : ∀ q, q ≠ pc → g q = f q) :
    ((List.range n).map g).sum + f pc = ((List.range n).map f).sum + g pc := by
  induction n with
  | zero => omega
  | succ n ih =>
    rw [List.range_succ, List.map_append, List.sum_append, List.map_append, List.sum_append]
    simp only [List.map_cons, List.map_nil, List.sum_cons, List.sum_nil, Nat.add_zero]
    by_cases h : pc = n
    · subst h
      have hle : ((List.range pc).map g).sum = ((List.range pc).map f).sum := by
        congr 1
        apply List.map_congr_left
        intro q hq
        exact hrest q (by rw [List.mem_range] at hq; omega)
      omega
    · have := ih (by omega)
      rw [hrest n (Ne.symm h)]
      omega

/-- The capacity sum after replacing one slot. -/
theorem capSum_setSlot (p : Prog) (st : St) (si : Nat) (sl : Slot) (hsi : si < p.ring)
    (hsize : st.slots.size = p.ring) :
    capSum p (st.setSlot si sl) + (st.slot si).activeCap = capSum p st + sl.activeCap := by
  unfold capSum
  have := sum_range_update (fun sj => (st.slot sj).activeCap) (fun sj => ((st.setSlot si sl).slot sj).activeCap)
    p.ring si hsi (fun q hq => by rw [St.slot_setSlot_ne _ _ _ _ (Ne.symm hq)])
  rw [St.slot_setSlot_self _ _ _ (hsize ▸ hsi)] at this
  simp only [St.queueCap_setSlot, St.aheadCap_setSlot]
  omega

@[simp] theorem capSum_charge (p : Prog) (st : St) (b : Nat) : capSum p (st.charge b) = capSum p st := rfl
@[simp] theorem capSum_mk_m (p : Prog) (st : St) (m : Meter) : capSum p ({ st with m := m } : St) = capSum p st := rfl

theorem GlobalInv.mk_m (h : GlobalInv p st) (m : Meter) (hm : m.allocBytes = st.m.allocBytes) :
    GlobalInv p { st with m := m } :=
  ⟨h.slotsSize, h.slots, h.queueLt, h.queueLen, h.queueCap, h.aheadLen, h.aheadCap, by
    simp only [capSum_mk_m]; rw [hm]; exact h.alloc⟩

theorem GlobalInv.slot_lt (h : GlobalInv p st) (hsi : si < p.ring) : si < st.slots.size := by
  rw [h.slotsSize]; exact hsi

/-- `storeInto` keeps a slot's invariant when the instruction is inside the program. -/
theorem storeInto_slotInv (p : Prog) (sl : Slot) (pc start : Nat) (ctr : List Nat) (hinv : SlotInv p sl)
    (hpc : pc < p.n) :
    SlotInv p (storeInto sl pc start ctr).1 ∧
    (storeInto sl pc start ctr).2 + 8 * sl.activeCap ≤ 8 * (storeInto sl pc start ctr).1.activeCap := by
  have hsize := hinv.size
  unfold storeInto
  split
  · rename_i hf
    refine ⟨⟨by rw [Array.size_setIfInBounds]; exact hsize, hinv.nodup, ?_, hinv.cap⟩, by simp⟩
    intro q hq
    obtain ⟨h1, h2⟩ := hinv.mem q hq
    refine ⟨h1, ?_⟩
    by_cases hqpc : pc = q
    · subst hqpc
      simp only [Slot.entry_eq, entry_set_self _ _ _ (hsize ▸ hpc)]
      simp only [beq_iff_eq] at hf
      exact hf
    · simp only [Slot.entry_eq, entry_set_ne _ _ _ _ hqpc]
      exact h2
  · rename_i hs
    simp only [beq_iff_eq] at hs
    have hnotmem : pc ∉ sl.active := fun hmem => hs (hinv.mem pc hmem).2
    have hnodup : (sl.active ++ [pc]).Nodup := by
      rw [List.nodup_append]
      refine ⟨hinv.nodup, List.nodup_cons.mpr ⟨by simp, List.nodup_nil⟩, ?_⟩
      intro a ha b hb
      simp only [List.mem_singleton] at hb
      subst hb
      intro hab
      exact hnotmem (hab ▸ ha)
    have hlen : sl.active.length + 1 ≤ p.n := by
      have := nodup_length_le (sl.active ++ [pc]) p.n hnodup (by
        intro x hx
        simp only [List.mem_append, List.mem_singleton] at hx
        rcases hx with hx | hx
        · exact (hinv.mem x hx).1
        · subst hx; exact hpc)
      simpa using this
    refine ⟨⟨by rw [Array.size_setIfInBounds]; exact hsize, hnodup, ?_,
      growCapAfter_le sl.active.length sl.activeCap p.n hinv.cap hlen⟩, grow_pot _ _⟩
    intro q hq
    simp only [List.mem_append, List.mem_singleton] at hq
    rcases hq with hq | hq
    · obtain ⟨h1, h2⟩ := hinv.mem q hq
      refine ⟨h1, ?_⟩
      have hne : pc ≠ q := fun h => hnotmem (h ▸ hq)
      simp only [Slot.entry_eq, entry_set_ne _ _ _ _ hne]
      exact h2
    · subst hq
      simp only [Slot.entry_eq, entry_set_self _ _ _ (hsize ▸ hpc)]
      exact ⟨hpc, trivial⟩

/-- A store keeps the global invariant. -/
theorem store_globalInv (p : Prog) (st : St) (si pc start : Nat) (ctr : List Nat) (hinv : GlobalInv p st)
    (hsi : si < p.ring) (hpc : pc < p.n) :
    GlobalInv p ((st.setSlot si (storeInto (st.slot si) pc start ctr).1).charge
      (storeInto (st.slot si) pc start ctr).2) := by
  obtain ⟨hsl, hpot⟩ := storeInto_slotInv p (st.slot si) pc start ctr (hinv.slots si hsi) hpc
  have hcap := capSum_setSlot p st si (storeInto (st.slot si) pc start ctr).1 hsi hinv.slotsSize
  refine ⟨by simp [hinv.slotsSize], ?_, by simpa using hinv.queueLt, by simpa using hinv.queueLen,
    hinv.queueCap, hinv.aheadLen, hinv.aheadCap, ?_⟩
  · intro sj hsj
    by_cases h : si = sj
    · subst h
      rw [St.slot_charge, St.slot_setSlot_self _ _ _ (hinv.slot_lt hsi)]
      exact hsl
    · rw [St.slot_charge, St.slot_setSlot_ne _ _ _ _ h]
      exact hinv.slots sj hsj
  · simp only [St.allocBytes_charge, capSum_charge, St.m_setSlot]
    have := hinv.alloc
    omega

theorem paStore_globalInv (p : Prog) (st : St) (si pc start : Nat) (ctr : List Nat) (hinv : GlobalInv p st)
    (hsi : si < p.ring) (hpc : pc < p.n) :
    GlobalInv p (paStore p.k st si pc start ctr).2 ∧ (paStore p.k st si pc start ctr).2.queue = st.queue := by
  rcases paStore_cases p.k st si pc start ctr with h | ⟨h, _⟩
  · rw [h]; exact ⟨hinv, rfl⟩
  · rw [h]; exact ⟨store_globalInv p st si pc start ctr hinv hsi hpc, rfl⟩

theorem capSum_pushQueue (p : Prog) (st : St) (pc : Nat) :
    capSum p (st.pushQueue pc) + st.queueCap = capSum p st + growCapAfter st.queue.length st.queueCap := by
  simp only [capSum, St.pushQueue, St.charge, St.slot]
  omega

theorem pushQueue_globalInv (p : Prog) (st : St) (pc : Nat) (hinv : GlobalInv p st) (hpc : pc < p.n)
    (hlen : st.queue.length + 1 ≤ 2 * p.n + 1) : GlobalInv p (st.pushQueue pc) := by
  refine ⟨by simpa using hinv.slotsSize, fun si hsi => by simpa using hinv.slots si hsi, ?_,
    by simpa using hlen, growCapAfter_queue _ _ _ hinv.queueCap hlen, hinv.aheadLen, hinv.aheadCap, ?_⟩
  · intro q hq
    simp only [St.queue_pushQueue, List.mem_cons] at hq
    rcases hq with hq | hq
    · subst hq; exact hpc
    · exact hinv.queueLt q hq
  · have hpot := grow_pot st.queue.length st.queueCap
    have halloc := hinv.alloc
    have hc := capSum_pushQueue p st pc
    have ha : (st.pushQueue pc).m.allocBytes = st.m.allocBytes + growBytes st.queue.length st.queueCap := rfl
    omega

theorem paRelax_globalInv (p : Prog) (st : St) (si pc start : Nat) (ctr : List Nat) (hinv : GlobalInv p st)
    (hsi : si < p.ring) (hpc : pc < p.n) (hlen : st.queue.length + 1 ≤ 2 * p.n + 1) :
    GlobalInv p (paRelax p.k st si pc start ctr) ∧
    (paRelax p.k st si pc start ctr).queue.length ≤ st.queue.length + 1 := by
  simp only [paRelax]
  have hinv0 := hinv.mk_m { st.m with relaxes := st.m.relaxes + 1 } rfl
  rcases paStore_cases p.k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr with
    h | ⟨h, _⟩
  · rw [h]; exact ⟨hinv0, by simp⟩
  · rw [h]
    simp only [ite_true]
    have hg := store_globalInv p _ si pc start ctr hinv0 hsi hpc
    refine ⟨pushQueue_globalInv p _ pc hg hpc (by simpa using hlen), by simp⟩

theorem compactQueue_globalInv (p : Prog) (st : St) (hinv : GlobalInv p st) :
    GlobalInv p (compactQueue st) ∧ (compactQueue st).queue.length ≤ p.n := by
  have hlen := compactQueue_queue_len st p.n hinv.queueLt
  refine ⟨⟨hinv.slotsSize, hinv.slots, fun pc hpc => hinv.queueLt pc ((compactQueue_mem st pc).mp hpc),
    by omega, hinv.queueCap, hinv.aheadLen, hinv.aheadCap, ?_⟩, hlen⟩
  simpa [compactQueue, capSum, St.slot] using hinv.alloc

theorem paConsider_globalInv (p : Prog) (st : St) (start : Nat) (ctr : List Nat) (e : Nat)
    (hinv : GlobalInv p st) : GlobalInv p (paConsider p.k st start ctr e) := by
  have hm := paConsider_m p.k st start ctr e
  refine ⟨by simpa using hinv.slotsSize, fun si hsi => by simpa using hinv.slots si hsi,
    by simpa using hinv.queueLt, by simpa using hinv.queueLen, ?_, ?_, ?_, ?_⟩
  · show (considerCore p.k _ start ctr e).queueCap ≤ _
    have : ∀ st', (considerCore p.k st' start ctr e).queueCap = st'.queueCap := by
      intro st'; unfold considerCore; split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl
    rw [this]; exact hinv.queueCap
  · show (considerCore p.k _ start ctr e).ahead.length ≤ _
    have : ∀ st', (considerCore p.k st' start ctr e).ahead = st'.ahead := by
      intro st'; unfold considerCore; split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl
    rw [this]; exact hinv.aheadLen
  · show (considerCore p.k _ start ctr e).aheadCap ≤ _
    have : ∀ st', (considerCore p.k st' start ctr e).aheadCap = st'.aheadCap := by
      intro st'; unfold considerCore; split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl
    rw [this]; exact hinv.aheadCap
  · have h1 : ∀ st', capSum p (considerCore p.k st' start ctr e) = capSum p st' := by
      intro st'
      have hs := considerCore_slots p.k st' start ctr e
      have hq : (considerCore p.k st' start ctr e).queueCap = st'.queueCap := by
        unfold considerCore; split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl
      have ha : (considerCore p.k st' start ctr e).aheadCap = st'.aheadCap := by
        unfold considerCore; split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl
      simp [capSum, St.slot, hs, hq, ha]
    rw [hm]
    show _ + 128 ≤ _ + 8 * capSum p (considerCore p.k _ start ctr e)
    rw [h1]
    simpa using hinv.alloc

theorem handleOp_globalInv (p : Prog) (st : St) (si pc start : Nat) (ctr : List Nat) (hwf : p.wf)
    (hinv : GlobalInv p st) (hsi : si < p.ring) (hpc : pc < p.n) (hlen : st.queue.length + 2 ≤ 2 * p.n + 1) :
    GlobalInv p (handleOp p si st pc start ctr) := by
  have hnext := (hwf.2 pc hpc).1
  have halt := (hwf.2 pc hpc).2
  clear hpc
  unfold handleOp
  split
  · obtain ⟨h1, hl1⟩ := paRelax_globalInv p st si _ start ctr hinv hsi hnext (by omega)
    exact (paRelax_globalInv p _ si _ start ctr h1 hsi halt (by omega)).1
  · exact (paRelax_globalInv p st si _ start ctr hinv hsi hnext (by omega)).1
  · split
    · exact (paRelax_globalInv p st si _ start ctr hinv hsi hnext (by omega)).1
    · exact hinv
  · split
    · exact (paRelax_globalInv p st si _ start ctr hinv hsi hnext (by omega)).1
    · exact hinv
  · exact paConsider_globalInv p st start ctr st.pos hinv
  · exact hinv

theorem closureStep_globalInv (p : Prog) (st : St) (si : Nat) (hwf : p.wf) (hinv : GlobalInv p st)
    (hsi : si < p.ring) (hn : 1 ≤ p.n) : GlobalInv p (closureStep p st si) := by
  unfold closureStep
  have hstep : ∃ st1, (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = st1 ∧
      GlobalInv p st1 ∧ st1.queue.length ≤ 2 * p.n := by
    split
    · obtain ⟨h1, h2⟩ := compactQueue_globalInv p st hinv
      exact ⟨_, rfl, h1, by omega⟩
    · rename_i hc
      exact ⟨st, rfl, hinv, by simp [queueCompactFactor] at hc; omega⟩
  obtain ⟨st1, hst1, hinv1, hlen1⟩ := hstep
  rw [hst1]
  unfold drain
  split
  · exact hinv1
  · rename_i pc rest hq
    have hinv2 : GlobalInv p (st1.popQueue rest) :=
      ⟨hinv1.slotsSize, hinv1.slots, fun q hq' => hinv1.queueLt q (by rw [hq]; exact List.mem_cons_of_mem _ hq'),
       by simp only [St.queue_popQueue]; rw [hq] at hlen1; simp at hlen1; omega,
       hinv1.queueCap, hinv1.aheadLen, hinv1.aheadCap, by simpa [St.popQueue, capSum, St.slot] using hinv1.alloc⟩
    have hpc : pc < p.n := hinv1.queueLt pc (by rw [hq]; simp)
    unfold handle
    split
    · exact hinv2
    · apply handleOp_globalInv p _ si pc _ _ hwf hinv2 hsi hpc
      simp only [St.queue_popQueue]
      rw [hq] at hlen1; simp at hlen1; omega

theorem paClosure_globalInv (p : Prog) (si : Nat) (hwf : p.wf) (hsi : si < p.ring) (hn : 1 ≤ p.n) :
    ∀ (fuel : Nat) (st : St), GlobalInv p st → GlobalInv p (paClosure p si st fuel) := by
  intro fuel
  induction fuel with
  | zero => intro st h; simpa [paClosure] using h
  | succ fuel ih =>
    intro st h
    simp only [paClosure]
    split
    · exact h
    · exact ih _ (closureStep_globalInv p st si hwf h hsi hn)

/-! ## The step figure split into the closure's share and the rest -/

/-- The part of the step figure the closure leaves alone. -/
def restFigure (m : Meter) (k ring atom : Nat) : Nat :=
  24 + ring + m.boundaries * (14 + 2 * ring) + m.skipped + m.filter +
  m.tests * (atom + 2) + m.arrivals * (4 * k + 12) + m.aheadWork + m.pending * 2

theorem stepFigure_split (m : Meter) (k ring atom : Nat) :
    stepFigure m k ring atom = restFigure m k ring atom + ccost k m := by
  unfold stepFigure restFigure ccost
  omega

/-- The counters outside the closure's share. -/
def restCounters (m : Meter) : Nat × Nat × Nat × Nat × Nat × Nat × Nat :=
  (m.boundaries, m.skipped, m.filter, m.tests, m.arrivals, m.aheadWork, m.pending)

theorem restFigure_of_restCounters (m m' : Meter) (k ring atom : Nat) (h : restCounters m' = restCounters m) :
    restFigure m' k ring atom = restFigure m k ring atom := by
  simp only [restCounters, Prod.mk.injEq] at h
  obtain ⟨h1, h2, h3, h4, h5, h6, h7⟩ := h
  simp only [restFigure, h1, h2, h3, h4, h5, h6, h7]

theorem restCounters_paStore (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    restCounters (paStore k st si pc start ctr).2.m = restCounters st.m := by
  rcases paStore_cases k st si pc start ctr with h | ⟨h, _⟩ <;> rw [h] <;> rfl

theorem restCounters_paRelax (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    restCounters (paRelax k st si pc start ctr).m = restCounters st.m := by
  simp only [paRelax]
  rcases paStore_cases k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr with
    h | ⟨h, _⟩ <;> rw [h] <;> rfl

theorem restCounters_paConsider (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    restCounters (paConsider k st start ctr e).m = restCounters st.m := by
  rw [paConsider_m]; rfl

theorem restCounters_handleOp (p : Prog) (si : Nat) (st : St) (pc start : Nat) (ctr : List Nat) :
    restCounters (handleOp p si st pc start ctr).m = restCounters st.m := by
  unfold handleOp
  split
  · rw [restCounters_paRelax, restCounters_paRelax]
  · rw [restCounters_paRelax]
  · split
    · rw [restCounters_paRelax]
    · rfl
  · split
    · rw [restCounters_paRelax]
    · rfl
  · rw [restCounters_paConsider]
  · rfl

theorem restCounters_closureStep (p : Prog) (st : St) (si : Nat) :
    restCounters (closureStep p st si).m = restCounters st.m := by
  unfold closureStep
  have hc : restCounters (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).m =
      restCounters st.m := by
    split <;> rfl
  generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = st1 at hc ⊢
  rw [← hc]
  unfold drain
  split
  · rfl
  · unfold handle
    split
    · rfl
    · rw [restCounters_handleOp]; rfl

theorem restCounters_paClosure (p : Prog) (si : Nat) :
    ∀ (fuel : Nat) (st : St), restCounters (paClosure p si st fuel).m = restCounters st.m := by
  intro fuel
  induction fuel with
  | zero => intro st; rfl
  | succ fuel ih =>
    intro st
    simp only [paClosure]
    split
    · rfl
    · rw [ih, restCounters_closureStep]

/-- The closure, priced: the potential at entry bounds the added cost. -/
theorem paClosure_stepFigure (p : Prog) (V : List Payload) (si : Nat) (hwf : p.wf) (fuel : Nat) (st : St)
    (atom : Nat) (hinv : ClosureInv p V si st) :
    stepFigure (paClosure p si st fuel).m p.k p.ring atom ≤
      stepFigure st.m p.k p.ring atom + weight p.k * measure p V si st := by
  obtain ⟨_, hcost, _⟩ := paClosure_bound p V si hwf fuel st hinv
  rw [stepFigure_split, stepFigure_split,
    restFigure_of_restCounters _ _ _ _ _ (restCounters_paClosure p si fuel st)]
  omega

/-! ## Arrivals and the consuming transitions -/

/-- Resetting a future slot for a new generation keeps its invariant. -/
theorem SlotInv.reset (h : SlotInv p sl) (g : Nat) : SlotInv p { sl with gen := g, active := [] } :=
  ⟨h.size, List.nodup_nil, fun pc hpc => by simp at hpc, h.cap⟩

theorem capSum_reset (p : Prog) (st : St) (fi g : Nat) (hfi : fi < p.ring) (hsize : st.slots.size = p.ring) :
    capSum p (st.setSlot fi { st.slot fi with gen := g, active := [] }) = capSum p st := by
  have := capSum_setSlot p st fi { st.slot fi with gen := g, active := [] } hfi hsize
  have h2 : ({ st.slot fi with gen := g, active := [] } : Slot).activeCap = (st.slot fi).activeCap := rfl
  rw [h2] at this
  omega

theorem paArrive_globalInv (p : Prog) (st : St) (pc delta start : Nat) (ctr : List Nat) (hwf : p.wf)
    (hinv : GlobalInv p st) (hpc : pc < p.n) (hring : 1 ≤ p.ring) :
    GlobalInv p (paArrive p st pc delta start ctr) ∧
    (paArrive p st pc delta start ctr).queue = st.queue := by
  unfold paArrive
  have hfi : (st.ci + delta) % p.ring < p.ring := Nat.mod_lt _ hring
  have hinv0 := hinv.mk_m { st.m with arrivals := st.m.arrivals + 1 } rfl
  have hreset : ∀ st1 : St, GlobalInv p st1 →
      GlobalInv p (if (st1.slot ((st.ci + delta) % p.ring)).gen != paGen (st.ci + delta) then
        st1.setSlot ((st.ci + delta) % p.ring)
          { st1.slot ((st.ci + delta) % p.ring) with gen := paGen (st.ci + delta), active := [] }
      else st1) := by
    intro st1 h1
    split
    · refine ⟨by simpa using h1.slotsSize, ?_, by simpa using h1.queueLt, by simpa using h1.queueLen,
        h1.queueCap, h1.aheadLen, h1.aheadCap, ?_⟩
      · intro sj hsj
        by_cases hs : (st.ci + delta) % p.ring = sj
        · subst hs
          rw [St.slot_setSlot_self _ _ _ (h1.slot_lt hfi)]
          exact (h1.slots _ hfi).reset _
        · rw [St.slot_setSlot_ne _ _ _ _ hs]
          exact h1.slots sj hsj
      · rw [capSum_reset p st1 _ _ hfi h1.slotsSize]
        simpa using h1.alloc
    · exact h1
  have h2 := hreset _ hinv0
  have hnext := (hwf.2 pc hpc).1
  refine ⟨(paStore_globalInv p _ _ _ start _ h2 hfi hnext).1, ?_⟩
  rw [(paStore_globalInv p _ _ _ start _ h2 hfi hnext).2]
  split <;> rfl

theorem paStore_m_ex (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    ∃ X, (paStore k st si pc start ctr).2.m = { st.m with allocBytes := X } := by
  rcases paStore_cases k st si pc start ctr with h | ⟨h, _⟩
  · rw [h]; exact ⟨st.m.allocBytes, rfl⟩
  · rw [h]; exact ⟨_, rfl⟩

theorem stepFigure_allocBytes (m : Meter) (X k ring atom : Nat) :
    stepFigure { m with allocBytes := X } k ring atom = stepFigure m k ring atom := rfl

theorem stepFigure_paArrive (p : Prog) (st : St) (pc delta start : Nat) (ctr : List Nat) (atom : Nat) :
    stepFigure (paArrive p st pc delta start ctr).m p.k p.ring atom =
      stepFigure st.m p.k p.ring atom + (4 * p.k + 12) := by
  unfold paArrive
  have key : ∀ (st1 : St) (fi tgt : Nat) (c : List Nat),
      st1.m = { st.m with arrivals := st.m.arrivals + 1 } →
      stepFigure (paStore p.k st1 fi tgt start c).2.m p.k p.ring atom =
        stepFigure st.m p.k p.ring atom + (4 * p.k + 12) := by
    intro st1 fi tgt c hm
    obtain ⟨X, hX⟩ := paStore_m_ex p.k st1 fi tgt start c
    rw [hX, stepFigure_allocBytes, hm]
    unfold stepFigure
    simp only [Nat.add_mul, Nat.one_mul]
    omega
  apply key
  split <;> rfl

/-! ## The lookahead -/

@[simp] theorem St.slots_pushAhead (st : St) (r : Int) : (st.pushAhead r).slots = st.slots := rfl
@[simp] theorem St.slot_pushAhead (st : St) (r : Int) (si : Nat) : (st.pushAhead r).slot si = st.slot si := rfl
@[simp] theorem St.queue_pushAhead (st : St) (r : Int) : (st.pushAhead r).queue = st.queue := rfl
@[simp] theorem St.queueCap_pushAhead (st : St) (r : Int) : (st.pushAhead r).queueCap = st.queueCap := rfl
@[simp] theorem St.slots_resetAhead (st : St) : st.resetAhead.slots = st.slots := rfl
@[simp] theorem St.slot_resetAhead (st : St) (si : Nat) : st.resetAhead.slot si = st.slot si := rfl
@[simp] theorem St.queue_resetAhead (st : St) : st.resetAhead.queue = st.queue := rfl
@[simp] theorem St.slots_bumpTests (st : St) : st.bumpTests.slots = st.slots := rfl
@[simp] theorem St.slot_bumpTests (st : St) (si : Nat) : st.bumpTests.slot si = st.slot si := rfl
@[simp] theorem St.queue_bumpTests (st : St) : st.bumpTests.queue = st.queue := rfl

theorem pushAhead_step (p : Prog) (st : St) (r : Int) (atom : Nat) (hinv : GlobalInv p st)
    (hlen : st.ahead.length + 1 ≤ maxElemAhead) :
    GlobalInv p (st.pushAhead r) ∧
    stepFigure (st.pushAhead r).m p.k p.ring atom = stepFigure st.m p.k p.ring atom + 2 ∧
    (st.pushAhead r).ahead.length = st.ahead.length + 1 := by
  refine ⟨⟨by simpa using hinv.slotsSize, fun si hsi => by simpa using hinv.slots si hsi,
    by simpa using hinv.queueLt, by simpa using hinv.queueLen, hinv.queueCap, ?_,
    growCapAfter_ahead _ _ hinv.aheadCap hlen, ?_⟩, ?_, ?_⟩
  · simp only [St.pushAhead, St.ahead_charge, List.length_append, List.length_singleton]
    omega
  · have hpot := grow_pot st.ahead.length st.aheadCap
    have halloc := hinv.alloc
    simp only [St.pushAhead, St.allocBytes_charge, capSum, St.charge, St.slot] at halloc ⊢
    omega
  · simp only [stepFigure, St.pushAhead, St.charge]
    omega
  · simp [St.pushAhead]

theorem decodeAheadFrom_step (p : Prog) (input : Input) (atom : Nat) :
    ∀ (fuel at_ : Nat) (st : St), GlobalInv p st → st.ahead.length + fuel ≤ maxElemAhead →
      GlobalInv p (decodeAheadFrom input.bytes at_ st fuel) ∧
      stepFigure (decodeAheadFrom input.bytes at_ st fuel).m p.k p.ring atom ≤
        stepFigure st.m p.k p.ring atom + 2 * fuel ∧
      (decodeAheadFrom input.bytes at_ st fuel).queue = st.queue := by
  intro fuel
  induction fuel with
  | zero => intro at_ st h _; exact ⟨h, by simp [decodeAheadFrom], rfl⟩
  | succ fuel ih =>
    intro at_ st hinv hlen
    simp only [decodeAheadFrom]
    split
    · obtain ⟨hg, hc, hl⟩ := pushAhead_step p st (decodeRuneAt input.bytes at_).1 atom hinv (by omega)
      obtain ⟨h1, h2, h3⟩ := ih (at_ + (decodeRuneAt input.bytes at_).2) _ hg (by rw [hl]; omega)
      exact ⟨h1, by omega, by rw [h3, St.queue_pushAhead]⟩
    · exact ⟨hinv, by omega, rfl⟩

theorem resetAhead_step (p : Prog) (st : St) (atom : Nat) (hinv : GlobalInv p st) :
    GlobalInv p st.resetAhead ∧
    stepFigure st.resetAhead.m p.k p.ring atom = stepFigure st.m p.k p.ring atom + 2 := by
  refine ⟨⟨hinv.slotsSize, hinv.slots, hinv.queueLt, hinv.queueLen, hinv.queueCap, by simp [St.resetAhead],
    hinv.aheadCap, by simpa [St.resetAhead, capSum, St.slot] using hinv.alloc⟩, ?_⟩
  simp only [stepFigure, St.resetAhead]; omega

theorem decodeAhead_step (p : Prog) (input : Input) (st : St) (atom : Nat) (hinv : GlobalInv p st) :
    GlobalInv p (decodeAhead input st) ∧
    stepFigure (decodeAhead input st).m p.k p.ring atom ≤ stepFigure st.m p.k p.ring atom + 18 ∧
    (decodeAhead input st).queue = st.queue := by
  unfold decodeAhead
  obtain ⟨hr, hc⟩ := resetAhead_step p st atom hinv
  obtain ⟨h1, h2, h3⟩ := decodeAheadFrom_step p input atom maxElemAhead st.pos _ hr (by simp [St.resetAhead])
  refine ⟨h1, ?_, by rw [h3, St.queue_resetAhead]⟩
  have h8 : 2 * maxElemAhead = 16 := rfl
  omega

/-! ## The consuming transitions -/

/-- Well-formed atom tests: a bracket probes at most `ring - 2` multi-character lengths. -/
def Atoms.wf (p : Prog) (atoms : Atoms) : Prop := ∀ pc, (atoms.lens pc).length ≤ p.ring - 2

theorem probeLens_step (p : Prog) (atoms : Atoms) (hwf : p.wf) (hring : 1 ≤ p.ring) (atom : Nat) :
    ∀ (lens : List Nat) (st : St) (pc start : Nat) (ctr : List Nat), GlobalInv p st → pc < p.n →
      GlobalInv p (probeLens p atoms st pc start ctr lens) ∧
      stepFigure (probeLens p atoms st pc start ctr lens).m p.k p.ring atom ≤
        stepFigure st.m p.k p.ring atom + lens.length * (4 * p.k + 12) ∧
      (probeLens p atoms st pc start ctr lens).queue = st.queue := by
  intro lens
  induction lens with
  | nil => intro st pc start ctr h _; exact ⟨h, by simp [probeLens], rfl⟩
  | cons len rest ih =>
    intro st pc start ctr hinv hpc
    simp only [probeLens, List.length_cons, Nat.add_mul, Nat.one_mul]
    split
    · obtain ⟨hg, hq⟩ := paArrive_globalInv p st pc len start ctr hwf hinv hpc hring
      obtain ⟨hg2, hc2, hq2⟩ := ih _ pc start ctr hg hpc
      rw [stepFigure_paArrive] at hc2
      exact ⟨hg2, by omega, by rw [hq2, hq]⟩
    · obtain ⟨hg2, hc2, hq2⟩ := ih _ pc start ctr hinv hpc
      exact ⟨hg2, by omega, hq2⟩

/-- The lookahead potential: eighteen units are owed once, until the lookahead is decoded. -/
def aheadPot (ready : Bool) : Nat := if ready then 0 else 18

theorem aheadPot_le (b : Bool) : aheadPot b ≤ 18 := by unfold aheadPot; split <;> omega

theorem perTest_multi (p : Prog) (atom : Nat) (hring : 2 ≤ p.ring) (L : Nat) (hL : L ≤ p.ring - 2) :
    atom + 2 + (4 * p.k + 12) + L * (4 * p.k + 12) ≤ perTest p atom := by
  unfold perTest
  have hr : p.ring - 1 = (p.ring - 2) + 1 := by omega
  have := Nat.mul_le_mul_right (4 * p.k + 12) hL
  rw [hr, Nat.add_mul, Nat.one_mul]
  omega

theorem bumpTests_step (p : Prog) (st : St) (atom : Nat) (hinv : GlobalInv p st) :
    GlobalInv p st.bumpTests ∧
    stepFigure st.bumpTests.m p.k p.ring atom = stepFigure st.m p.k p.ring atom + (atom + 2) := by
  refine ⟨hinv.mk_m _ rfl, ?_⟩
  simp only [stepFigure, St.bumpTests, Nat.add_mul, Nat.one_mul]; omega

theorem consumeArrive_step (p : Prog) (atoms : Atoms) (st : St) (pc start : Nat) (ctr : List Nat) (atom : Nat)
    (hwf : p.wf) (hring : 1 ≤ p.ring) (hinv : GlobalInv p st) (hpc : pc < p.n) :
    GlobalInv p (consumeArrive p atoms st pc start ctr) ∧
    stepFigure (consumeArrive p atoms st pc start ctr).m p.k p.ring atom ≤
      stepFigure st.m p.k p.ring atom + (4 * p.k + 12) ∧
    (consumeArrive p atoms st pc start ctr).queue = st.queue := by
  unfold consumeArrive
  split
  · obtain ⟨hg, hq⟩ := paArrive_globalInv p st pc 1 start ctr hwf hinv hpc hring
    exact ⟨hg, by rw [stepFigure_paArrive]; exact Nat.le_refl _, hq⟩
  · exact ⟨hinv, by omega, rfl⟩

theorem consumeProbe_step (p : Prog) (atoms : Atoms) (input : Input) (st : St) (aheadReady : Bool)
    (pc start : Nat) (ctr : List Nat) (atom : Nat) (hwf : p.wf) (hawf : atoms.wf p) (hring : 2 ≤ p.ring)
    (hinv : GlobalInv p st) (hpc : pc < p.n) :
    GlobalInv p (consumeProbe p atoms input st aheadReady pc start ctr).1 ∧
    stepFigure (consumeProbe p atoms input st aheadReady pc start ctr).1.m p.k p.ring atom +
        aheadPot (consumeProbe p atoms input st aheadReady pc start ctr).2 ≤
      stepFigure st.m p.k p.ring atom + aheadPot aheadReady + (p.ring - 2) * (4 * p.k + 12) ∧
    (consumeProbe p atoms input st aheadReady pc start ctr).1.queue = st.queue := by
  unfold consumeProbe
  by_cases hb : ((p.ins.getD pc default).op == Op.bracket && !(atoms.lens pc).isEmpty) = true
  · rw [if_pos hb]
    have hlens := hawf pc
    have hL : (atoms.lens pc).length * (4 * p.k + 12) ≤ (p.ring - 2) * (4 * p.k + 12) :=
      Nat.mul_le_mul_right _ hlens
    by_cases hr : aheadReady = true
    · rw [if_pos hr]
      obtain ⟨hg2, hc2, hq2⟩ := probeLens_step p atoms hwf (by omega) atom (atoms.lens pc) st pc start ctr hinv hpc
      refine ⟨hg2, ?_, hq2⟩
      subst hr
      simp only [aheadPot, ite_true]
      omega
    · rw [if_neg hr]
      simp only [Bool.not_eq_true] at hr
      subst hr
      obtain ⟨hg, hc, hq⟩ := decodeAhead_step p input st atom hinv
      obtain ⟨hg2, hc2, hq2⟩ := probeLens_step p atoms hwf (by omega) atom (atoms.lens pc) _ pc start ctr hg hpc
      refine ⟨hg2, ?_, by rw [hq2, hq]⟩
      simp only [aheadPot, ite_true, Bool.false_eq_true, ite_false]
      omega
  · rw [if_neg hb]
    exact ⟨hinv, by dsimp only; omega, rfl⟩

theorem consumeFresh_step (p : Prog) (atoms : Atoms) (input : Input) (st : St) (aheadReady : Bool)
    (pc start : Nat) (ctr : List Nat) (atom : Nat) (hwf : p.wf) (hawf : atoms.wf p) (hring : 2 ≤ p.ring)
    (hinv : GlobalInv p st) (hpc : pc < p.n) :
    GlobalInv p (consumeFresh p atoms input st aheadReady pc start ctr).1 ∧
    stepFigure (consumeFresh p atoms input st aheadReady pc start ctr).1.m p.k p.ring atom +
        aheadPot (consumeFresh p atoms input st aheadReady pc start ctr).2 ≤
      stepFigure st.m p.k p.ring atom + aheadPot aheadReady + (4 * p.k + 12) + (p.ring - 2) * (4 * p.k + 12) ∧
    (consumeFresh p atoms input st aheadReady pc start ctr).1.queue = st.queue := by
  unfold consumeFresh
  have hcons : GlobalInv p (consumeProbe p atoms input (consumeArrive p atoms st pc start ctr) aheadReady pc start ctr).1 ∧
      stepFigure (consumeProbe p atoms input (consumeArrive p atoms st pc start ctr) aheadReady pc start ctr).1.m
          p.k p.ring atom +
        aheadPot (consumeProbe p atoms input (consumeArrive p atoms st pc start ctr) aheadReady pc start ctr).2 ≤
      stepFigure st.m p.k p.ring atom + aheadPot aheadReady + (4 * p.k + 12) + (p.ring - 2) * (4 * p.k + 12) ∧
      (consumeProbe p atoms input (consumeArrive p atoms st pc start ctr) aheadReady pc start ctr).1.queue =
        st.queue := by
    obtain ⟨hg1, hc1, hq1⟩ := consumeArrive_step p atoms st pc start ctr atom hwf (by omega) hinv hpc
    obtain ⟨hg2, hc2, hq2⟩ := consumeProbe_step p atoms input _ aheadReady pc start ctr atom hwf hawf hring hg1 hpc
    exact ⟨hg2, by omega, by rw [hq2, hq1]⟩
  split
  · exact hcons
  · exact hcons
  · exact hcons
  · exact hcons
  · exact ⟨hinv, by dsimp only; omega, rfl⟩

theorem consumeOne_step (p : Prog) (atoms : Atoms) (input : Input) (si : Nat) (hwf : p.wf) (hawf : atoms.wf p)
    (hring : 2 ≤ p.ring) (atom : Nat) (st : St) (aheadReady : Bool) (pc : Nat) (hinv : GlobalInv p st)
    (hpc : pc < p.n) :
    GlobalInv p (consumeOne p atoms input si st aheadReady pc).1 ∧
    stepFigure (consumeOne p atoms input si st aheadReady pc).1.m p.k p.ring atom +
        aheadPot (consumeOne p atoms input si st aheadReady pc).2 ≤
      stepFigure st.m p.k p.ring atom + aheadPot aheadReady + perTest p atom ∧
    (consumeOne p atoms input si st aheadReady pc).1.queue = st.queue := by
  obtain ⟨hg0, hc0⟩ := bumpTests_step p st atom hinv
  have hpt := perTest_multi p atom hring (p.ring - 2) (Nat.le_refl _)
  unfold consumeOne
  split
  · exact ⟨hg0, by simp only []; omega, rfl⟩
  · obtain ⟨hg, hc, hq⟩ := consumeFresh_step p atoms input st.bumpTests aheadReady pc _ _ atom hwf hawf hring hg0 hpc
    exact ⟨hg, by omega, by rw [hq, St.queue_bumpTests]⟩

theorem consumeList_step (p : Prog) (atoms : Atoms) (input : Input) (si : Nat) (hwf : p.wf) (hawf : atoms.wf p)
    (hring : 2 ≤ p.ring) (atom : Nat) :
    ∀ (active : List Nat) (st : St) (aheadReady : Bool), GlobalInv p st → (∀ pc ∈ active, pc < p.n) →
      GlobalInv p (consumeList p atoms input si active st aheadReady) ∧
      stepFigure (consumeList p atoms input si active st aheadReady).m p.k p.ring atom ≤
        stepFigure st.m p.k p.ring atom + aheadPot aheadReady + active.length * perTest p atom ∧
      (consumeList p atoms input si active st aheadReady).queue = st.queue := by
  intro active
  induction active with
  | nil => intro st r h _; exact ⟨h, by simp [consumeList], rfl⟩
  | cons pc rest ih =>
    intro st aheadReady hinv hlt
    simp only [consumeList]
    obtain ⟨hg1, hc1, hq1⟩ := consumeOne_step p atoms input si hwf hawf hring atom st aheadReady pc hinv
      (hlt pc (by simp))
    obtain ⟨hg2, hc2, hq2⟩ := ih _ _ hg1 (fun q hq => hlt q (by simp [hq]))
    refine ⟨hg2, ?_, by rw [hq2, hq1]⟩
    simp only [List.length_cons, Nat.add_mul, Nat.one_mul]
    omega

theorem paConsume_step (p : Prog) (atoms : Atoms) (input : Input) (si : Nat) (hwf : p.wf) (hawf : atoms.wf p)
    (hring : 2 ≤ p.ring) (atom : Nat) (st : St) (hinv : GlobalInv p st) (hsi : si < p.ring) :
    GlobalInv p (paConsume p atoms input st si) ∧
    stepFigure (paConsume p atoms input st si).m p.k p.ring atom ≤
      stepFigure st.m p.k p.ring atom + 18 + p.n * perTest p atom ∧
    (paConsume p atoms input st si).queue = st.queue := by
  unfold paConsume
  have hsl := hinv.slots si hsi
  obtain ⟨h1, h2, h3⟩ := consumeList_step p atoms input si hwf hawf hring atom (st.slot si).active st false hinv
    (fun pc hpc => (hsl.mem pc hpc).1)
  refine ⟨h1, ?_, h3⟩
  have := hsl.active_len
  have : (st.slot si).active.length * perTest p atom ≤ p.n * perTest p atom := Nat.mul_le_mul_right _ this
  simp only [aheadPot, Bool.false_eq_true, ite_false] at h2
  omega

/-! ## The boundary -/

theorem decodeOne_size (bs : ByteArray) (i c size : Nat) (h : Ere.decodeOne bs i = some (c, size)) :
    1 ≤ size ∧ i + size ≤ bs.size := by
  unfold Ere.decodeOne at h
  split at h
  · rename_i h0
    simp only at h
    split at h
    · simp only [Option.some.injEq, Prod.mk.injEq] at h; omega
    · split at h
      · simp at h
      · split at h
        · split at h
          · rename_i h1
            split at h
            · simp only [Option.some.injEq, Prod.mk.injEq] at h
              split at h1 <;> simp_all <;> omega
            · simp at h
          · simp at h
        · split at h
          · split at h
            · rename_i h1 h2
              split at h
              · simp at h
              · split at h
                · simp at h
                · split at h
                  · simp at h
                  · simp only [Option.some.injEq, Prod.mk.injEq] at h
                    split at h1 <;> split at h2 <;> simp_all <;> omega
            · simp at h
          · split at h
            · split at h
              · rename_i h1 h2 h3
                split at h
                · simp at h
                · split at h
                  · simp at h
                  · split at h
                    · simp at h
                    · simp only [Option.some.injEq, Prod.mk.injEq] at h
                      split at h1 <;> split at h2 <;> split at h3 <;> simp_all <;> omega
              · simp at h
            · simp at h
  · simp at h

theorem decodeRuneAt_size (bs : ByteArray) (i : Nat) (hi : i < bs.size) :
    1 ≤ (decodeRuneAt bs i).2 ∧ i + (decodeRuneAt bs i).2 ≤ bs.size := by
  unfold decodeRuneAt
  split
  · rename_i c size h
    exact decodeOne_size bs i c size h
  · simp; omega

theorem scanAheadFrom_spec (p : Prog) (input : Input) :
    ∀ (fuel i : Nat), i ≤ input.bytes.size →
      i ≤ (scanAheadFrom p input i fuel).1 ∧ (scanAheadFrom p input i fuel).1 ≤ input.bytes.size ∧
      (scanAheadFrom p input i fuel).2 ≤ (scanAheadFrom p input i fuel).1 - i + 1 := by
  intro fuel
  induction fuel with
  | zero => intro i hi; simp [scanAheadFrom]; omega
  | succ fuel ih =>
    intro i hi
    simp only [scanAheadFrom]
    split
    · split
      · simp; omega
      · obtain ⟨h1, h2, h3⟩ := ih (i + 1) (by omega)
        simp only []
        omega
    · simp; omega

theorem scanAhead_spec (p : Prog) (input : Input) (pos : Nat) (h : pos ≤ input.bytes.size) :
    pos ≤ (scanAhead p input pos).1 ∧ (scanAhead p input pos).1 ≤ input.bytes.size ∧
    (scanAhead p input pos).2 ≤ (scanAhead p input pos).1 - pos + 1 :=
  scanAheadFrom_spec p input (input.bytes.size + 1) pos h

theorem pendingFrom_count (p : Prog) (st : St) :
    ∀ (fuel delta : Nat), (pendingFrom p st delta fuel).2 ≤ p.ring - delta := by
  intro fuel
  induction fuel with
  | zero => intro delta; simp [pendingFrom]
  | succ fuel ih =>
    intro delta
    simp only [pendingFrom]
    split
    · split
      · simp; omega
      · have := ih (delta + 1)
        simp only []
        omega
    · simp

theorem pendingFrom_le (p : Prog) (st : St) : (pendingFrom p st 1 p.ring).2 ≤ p.ring - 1 :=
  pendingFrom_count p st p.ring 1

/-- The measure at the start of a closure, from a queue of the live list and the spawn. -/
theorem measure_le (p : Prog) (V : List Payload) (si : Nat) (st : St) (hV : V.length ≤ p.n + 1) :
    measure p V si st ≤ st.queue.length + p.n * (p.n + 1) := by
  unfold measure
  have := rankSum_le p.k V (st.slot si) p.n
  have := Nat.mul_le_mul_left p.n hV
  omega


/-! Position preservation: nothing inside a boundary moves the position but the advance at its end. -/

theorem paRelax_pos (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    (paRelax k st si pc start ctr).pos = st.pos := by
  simp only [paRelax]
  rcases paStore_cases k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr with
    h | ⟨h, _⟩ <;> rw [h] <;> rfl

theorem paConsider_pos (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e : Nat) :
    (paConsider k st start ctr e).pos = st.pos := by
  simp only [paConsider]
  unfold considerCore
  split <;> (try split) <;> (try split) <;> (try split) <;> (try split) <;> rfl

theorem handleOp_pos (p : Prog) (si : Nat) (st : St) (pc start : Nat) (ctr : List Nat) :
    (handleOp p si st pc start ctr).pos = st.pos := by
  unfold handleOp
  split
  · rw [paRelax_pos, paRelax_pos]
  · rw [paRelax_pos]
  · split
    · rw [paRelax_pos]
    · rfl
  · split
    · rw [paRelax_pos]
    · rfl
  · rw [paConsider_pos]
  · rfl

theorem closureStep_pos (p : Prog) (st : St) (si : Nat) : (closureStep p st si).pos = st.pos := by
  have hcp : (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).pos = st.pos := by
    split <;> rfl
  rw [← hcp]
  unfold closureStep
  generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = s
  unfold drain
  split
  · rfl
  · unfold handle
    split
    · rfl
    · rw [handleOp_pos]; rfl

theorem paClosure_pos (p : Prog) (si : Nat) : ∀ (fuel : Nat) (st : St), (paClosure p si st fuel).pos = st.pos := by
  intro fuel
  induction fuel with
  | zero => intro st; rfl
  | succ fuel ih =>
    intro st
    simp only [paClosure]
    split
    · rfl
    · rw [ih, closureStep_pos]

theorem paStore_pos (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    (paStore k st si pc start ctr).2.pos = st.pos := by
  rcases paStore_cases k st si pc start ctr with h | ⟨h, _⟩ <;> rw [h] <;> rfl

theorem paArrive_pos (p : Prog) (st : St) (pc delta start : Nat) (ctr : List Nat) :
    (paArrive p st pc delta start ctr).pos = st.pos := by
  simp only [paArrive, paStore_pos]
  split <;> rfl

theorem probeLens_pos (p : Prog) (atoms : Atoms) (pc start : Nat) (ctr : List Nat) :
    ∀ (lens : List Nat) (st : St), (probeLens p atoms st pc start ctr lens).pos = st.pos := by
  intro lens
  induction lens with
  | nil => intro st; rfl
  | cons len rest ih =>
    intro st
    simp only [probeLens]
    rw [ih]
    split
    · rw [paArrive_pos]
    · rfl

theorem decodeAheadFrom_pos (bytes : ByteArray) :
    ∀ (fuel at_ : Nat) (st : St), (decodeAheadFrom bytes at_ st fuel).pos = st.pos := by
  intro fuel
  induction fuel with
  | zero => intro at_ st; rfl
  | succ fuel ih =>
    intro at_ st
    simp only [decodeAheadFrom]
    split
    · rw [ih]; rfl
    · rfl

theorem decodeAhead_pos (input : Input) (st : St) : (decodeAhead input st).pos = st.pos := by
  unfold decodeAhead; rw [decodeAheadFrom_pos]; rfl

theorem consumeProbe_pos (p : Prog) (atoms : Atoms) (input : Input) (st : St) (b : Bool) (pc start : Nat)
    (ctr : List Nat) : (consumeProbe p atoms input st b pc start ctr).1.pos = st.pos := by
  unfold consumeProbe
  split
  · simp only []
    rw [probeLens_pos]
    split
    · rfl
    · rw [decodeAhead_pos]
  · rfl

theorem consumeArrive_pos (p : Prog) (atoms : Atoms) (st : St) (pc start : Nat) (ctr : List Nat) :
    (consumeArrive p atoms st pc start ctr).pos = st.pos := by
  unfold consumeArrive
  split
  · rw [paArrive_pos]
  · rfl

theorem consumeFresh_pos (p : Prog) (atoms : Atoms) (input : Input) (st : St) (b : Bool) (pc start : Nat)
    (ctr : List Nat) : (consumeFresh p atoms input st b pc start ctr).1.pos = st.pos := by
  unfold consumeFresh
  split <;> (try rw [consumeProbe_pos, consumeArrive_pos]) <;> rfl

theorem consumeList_pos (p : Prog) (atoms : Atoms) (input : Input) (si : Nat) :
    ∀ (l : List Nat) (st : St) (b : Bool), (consumeList p atoms input si l st b).pos = st.pos := by
  intro l
  induction l with
  | nil => intro st b; rfl
  | cons pc rest ih =>
    intro st b
    simp only [consumeList]
    rw [ih]
    unfold consumeOne
    split
    · rfl
    · rw [consumeFresh_pos]; rfl

theorem paConsume_pos (p : Prog) (atoms : Atoms) (input : Input) (st : St) (si : Nat) :
    (paConsume p atoms input st si).pos = st.pos := by
  unfold paConsume; rw [consumeList_pos]

/-! The named boundary steps and what they leave alone. -/

@[simp] theorem St.slots_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).slots = st.slots := rfl
@[simp] theorem St.slot_setFlags (st : St) (i : Input) (v : Int) (si : Nat) : (st.setFlags i v).slot si = st.slot si := rfl
@[simp] theorem St.queue_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).queue = st.queue := rfl
@[simp] theorem St.queueCap_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).queueCap = st.queueCap := rfl
@[simp] theorem St.ahead_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).ahead = st.ahead := rfl
@[simp] theorem St.aheadCap_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).aheadCap = st.aheadCap := rfl
@[simp] theorem St.m_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).m = st.m := rfl
@[simp] theorem St.pos_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).pos = st.pos := rfl
@[simp] theorem St.matched_setFlags (st : St) (i : Input) (v : Int) : (st.setFlags i v).matched = st.matched := rfl

@[simp] theorem St.slots_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).slots = st.slots := by
  unfold St.copyQueue; split <;> rfl
@[simp] theorem St.slot_copyQueue (st : St) (a : List Nat) (si : Nat) : (st.copyQueue a).slot si = st.slot si := by
  simp [St.slot]
@[simp] theorem St.queue_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).queue = a.reverse := by
  unfold St.copyQueue; split <;> rfl
@[simp] theorem St.ahead_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).ahead = st.ahead := by
  unfold St.copyQueue; split <;> rfl
@[simp] theorem St.aheadCap_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).aheadCap = st.aheadCap := by
  unfold St.copyQueue; split <;> rfl
@[simp] theorem St.pos_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).pos = st.pos := by
  unfold St.copyQueue; split <;> rfl
@[simp] theorem St.matched_copyQueue (st : St) (a : List Nat) : (st.copyQueue a).matched = st.matched := by
  unfold St.copyQueue; split <;> rfl
theorem stepFigure_copyQueue (st : St) (a : List Nat) (k ring atom : Nat) :
    stepFigure (st.copyQueue a).m k ring atom = stepFigure st.m k ring atom := by
  unfold St.copyQueue; split <;> simp [stepFigure, St.charge]

@[simp] theorem St.slots_advance (st : St) (n : Nat) : (st.advance n).slots = st.slots := rfl
@[simp] theorem St.slot_advance (st : St) (n si : Nat) : (st.advance n).slot si = st.slot si := rfl
@[simp] theorem St.queue_advance (st : St) (n : Nat) : (st.advance n).queue = st.queue := rfl
@[simp] theorem St.m_advance (st : St) (n : Nat) : (st.advance n).m = st.m := rfl
@[simp] theorem St.pos_advance (st : St) (n : Nat) : (st.advance n).pos = st.pos + n := rfl
@[simp] theorem St.slots_bumpPending (st : St) (n : Nat) : (st.bumpPending n).slots = st.slots := rfl
@[simp] theorem St.slot_bumpPending (st : St) (n si : Nat) : (st.bumpPending n).slot si = st.slot si := rfl
@[simp] theorem St.queue_bumpPending (st : St) (n : Nat) : (st.bumpPending n).queue = st.queue := rfl
@[simp] theorem St.pos_bumpPending (st : St) (n : Nat) : (st.bumpPending n).pos = st.pos := rfl
@[simp] theorem St.slots_bumpBoundaries (st : St) : st.bumpBoundaries.slots = st.slots := rfl
@[simp] theorem St.slot_bumpBoundaries (st : St) (si : Nat) : st.bumpBoundaries.slot si = st.slot si := rfl
@[simp] theorem St.queue_bumpBoundaries (st : St) : st.bumpBoundaries.queue = st.queue := rfl
@[simp] theorem St.pos_bumpBoundaries (st : St) : st.bumpBoundaries.pos = st.pos := rfl
@[simp] theorem St.slots_bumpSkipped (st : St) (n : Nat) : (st.bumpSkipped n).slots = st.slots := rfl
@[simp] theorem St.slot_bumpSkipped (st : St) (n si : Nat) : (st.bumpSkipped n).slot si = st.slot si := rfl
@[simp] theorem St.queue_bumpSkipped (st : St) (n : Nat) : (st.bumpSkipped n).queue = st.queue := rfl
@[simp] theorem St.pos_bumpSkipped (st : St) (n : Nat) : (st.bumpSkipped n).pos = st.pos := rfl
@[simp] theorem St.slots_jumpTo (st : St) (n : Nat) : (st.jumpTo n).slots = st.slots := rfl
@[simp] theorem St.slot_jumpTo (st : St) (n si : Nat) : (st.jumpTo n).slot si = st.slot si := rfl
@[simp] theorem St.queue_jumpTo (st : St) (n : Nat) : (st.jumpTo n).queue = st.queue := rfl
@[simp] theorem St.m_jumpTo (st : St) (n : Nat) : (st.jumpTo n).m = st.m := rfl
@[simp] theorem St.pos_jumpTo (st : St) (n : Nat) : (st.jumpTo n).pos = n := rfl
@[simp] theorem St.slots_filterSlot (st : St) (si g : Nat) : (st.filterSlot si g).slots.size = st.slots.size := by
  simp [St.filterSlot, St.setSlot]
@[simp] theorem St.queue_filterSlot (st : St) (si g : Nat) : (st.filterSlot si g).queue = st.queue := rfl
@[simp] theorem St.pos_filterSlot (st : St) (si g : Nat) : (st.filterSlot si g).pos = st.pos := rfl

/-- A meter-only change keeps the global invariant; this covers every bump. -/
theorem GlobalInv.of_same (h : GlobalInv p st) (st' : St) (hs : st'.slots = st.slots) (hq : st'.queue = st.queue)
    (hqc : st'.queueCap = st.queueCap) (ha : st'.ahead = st.ahead) (hac : st'.aheadCap = st.aheadCap)
    (hm : st'.m.allocBytes = st.m.allocBytes) : GlobalInv p st' :=
  ⟨by rw [hs]; exact h.slotsSize, fun si hsi => by simp only [St.slot, hs]; exact h.slots si hsi,
   by rw [hq]; exact h.queueLt, by rw [hq]; exact h.queueLen, by rw [hqc]; exact h.queueCap,
   by rw [ha]; exact h.aheadLen, by rw [hac]; exact h.aheadCap, by
     have := h.alloc
     simp only [capSum, St.slot, hs, hqc, hac, hm]
     simpa [capSum, St.slot] using this⟩

theorem copyQueue_step (p : Prog) (st : St) (active : List Nat) (hinv : GlobalInv p st)
    (hlt : ∀ pc ∈ active, pc < p.n) (hlen : active.length ≤ p.n) : GlobalInv p (st.copyQueue active) := by
  unfold St.copyQueue
  split
  · rename_i hc
    refine ⟨by simpa using hinv.slotsSize, fun si hsi => by simpa [St.slot] using hinv.slots si hsi,
      fun pc hpc => hlt pc (List.mem_reverse.mp (by simpa using hpc)),
      by simp [List.length_reverse]; omega, ?_, hinv.aheadLen, hinv.aheadCap, ?_⟩
    · have := growCap_le st.queueCap active.length p.n hc hlen
      simp only [St.queueCap_charge]
      omega
    · have hd := growCap_doubles st.queueCap active.length
      have halloc := hinv.alloc
      simp only [St.allocBytes_charge, capSum, St.charge, St.slot] at halloc ⊢
      omega
  · exact ⟨hinv.slotsSize, fun si hsi => by simpa [St.slot] using hinv.slots si hsi,
      fun pc hpc => hlt pc (List.mem_reverse.mp hpc),
      by simp [List.length_reverse]; omega, hinv.queueCap, hinv.aheadLen, hinv.aheadCap,
      by simpa [capSum, St.slot] using hinv.alloc⟩

theorem spawn_step (p : Prog) (st : St) (si : Nat) (atom : Nat) (hwf : p.wf) (hinv : GlobalInv p st)
    (hsi : si < p.ring) (hlen : st.queue.length + 1 ≤ 2 * p.n + 1) :
    GlobalInv p (spawn p st si) ∧
    stepFigure (spawn p st si).m p.k p.ring atom ≤ stepFigure st.m p.k p.ring atom + (2 * p.k + 7) ∧
    (spawn p st si).queue.length ≤ st.queue.length + 1 ∧ (spawn p st si).pos = st.pos := by
  unfold spawn
  split
  · exact ⟨hinv, by omega, by omega, rfl⟩
  · obtain ⟨hg, hl⟩ := paRelax_globalInv p st si p.start st.pos _ hinv hsi hwf.1 hlen
    refine ⟨hg, ?_, hl, ?_⟩
    · have hr := restCounters_paRelax p.k st si p.start st.pos (List.replicate p.k 0)
      rw [stepFigure_split, stepFigure_split, restFigure_of_restCounters _ _ _ _ _ hr]
      have : ccost p.k (paRelax p.k st si p.start st.pos (List.replicate p.k 0)).m = ccost p.k st.m + (2 * p.k + 7) := by
        simp only [paRelax]
        rcases paStore_cases p.k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si p.start st.pos
          (List.replicate p.k 0) with h | ⟨h, _⟩
        · rw [h]; simp only [Bool.false_eq_true, ite_false, ccost, Nat.add_mul, Nat.one_mul]; omega
        · rw [h]
          simp only [ite_true, ccost, St.pops_pushQueue, St.compactWork_pushQueue, St.relaxes_pushQueue,
            St.considers_pushQueue, St.charge, St.setSlot, Nat.add_mul, Nat.one_mul]
          omega
      omega
    · simp only [paRelax]
      rcases paStore_cases p.k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si p.start st.pos
        (List.replicate p.k 0) with h | ⟨h, _⟩ <;> rw [h] <;> rfl

theorem closureInv_of_global (p : Prog) (st : St) (si : Nat) (x : Payload) (hg : GlobalInv p st)
    (hsi : si < p.ring) : ClosureInv p (knownPayloads x (st.slot si)) si st :=
  ⟨hg.slotsSize, hsi, (hg.slots si hsi).size, hg.queueLt, fun q hq hf =>
    fresh_mem_knownPayloads x (st.slot si) q (by rw [(hg.slots si hsi).size]; exact hq) hf⟩

theorem closeAt_step (p : Prog) (st : St) (si : Nat) (atom : Nat) (hwf : p.wf) (hinv : GlobalInv p st)
    (hsi : si < p.ring) (hn : 1 ≤ p.n) :
    GlobalInv p (closeAt p si st) ∧
    stepFigure (closeAt p si st).m p.k p.ring atom ≤
      stepFigure st.m p.k p.ring atom + weight p.k * (st.queue.length + p.n * (p.n + 1)) ∧
    (closeAt p si st).pos = st.pos := by
  unfold closeAt
  have hci := closureInv_of_global p st si (st.pos, List.replicate p.k 0) hinv hsi
  have hV : (knownPayloads (st.pos, List.replicate p.k 0) (st.slot si)).length ≤ p.n + 1 := by
    have := knownPayloads_length (st.pos, List.replicate p.k 0) (st.slot si)
    rw [(hinv.slots si hsi).size] at this
    exact this
  have hm := measure_le p _ si st hV
  have hc := paClosure_stepFigure p _ si hwf (closureFuel p st) st atom hci
  refine ⟨paClosure_globalInv p si hwf hsi hn _ st hinv, ?_, ?_⟩
  · have := Nat.mul_le_mul_left (weight p.k) hm
    omega
  · exact paClosure_pos p si _ st

theorem afterConsume_step (p : Prog) (st : St) (size : Nat) (atom : Nat) (hinv : GlobalInv p st) :
    let r := afterConsume p st size
    GlobalInv p r.1 ∧
    stepFigure r.1.m p.k p.ring atom ≤ stepFigure st.m p.k p.ring atom + 2 * (p.ring - 1) ∧
    (r.1.pos = st.pos ∨ r.1.pos = st.pos + size) ∧ (r.2.isSome → r.1.pos = st.pos + size) := by
  intro r
  have hpend := pendingFrom_le p st
  have hbump : ∀ c, stepFigure (st.bumpPending c).m p.k p.ring atom = stepFigure st.m p.k p.ring atom + 2 * c := by
    intro c; simp only [stepFigure, St.bumpPending]; omega
  have hbumpInv : ∀ c, GlobalInv p (st.bumpPending c) := fun c => hinv.of_same _ rfl rfl rfl rfl rfl rfl
  have hadvInv : ∀ st' : St, GlobalInv p st' → GlobalInv p (st'.advance size) := fun st' h =>
    h.of_same _ rfl rfl rfl rfl rfl rfl
  simp only [r, afterConsume]
  split
  · split
    · refine ⟨hadvInv _ (hbumpInv _), ?_, Or.inr rfl, fun _ => rfl⟩
      simp only [St.m_advance, hbump]
      have := Nat.mul_le_mul_left 2 hpend
      omega
    · refine ⟨hbumpInv _, ?_, Or.inl rfl, by simp⟩
      simp only [hbump]
      have := Nat.mul_le_mul_left 2 hpend
      omega
  · exact ⟨hadvInv _ hinv, by simp, Or.inr rfl, fun _ => rfl⟩

/-- The price of the body: the spawn, the closure, the consuming transitions, and the stop test. -/
def bodyCost (p : Prog) (atom : Nat) : Nat :=
  (2 * p.k + 7) + weight p.k * (p.n + 1) * (p.n + 1) + 18 + p.n * perTest p atom + 2 * (p.ring - 1)

theorem boundaryBody_step (p : Prog) (atoms : Atoms) (input : Input) (st : St) (si : Nat) (prev : Int)
    (active : List Nat) (atom : Nat) (hwf : p.wf) (hawf : atoms.wf p) (hring : 2 ≤ p.ring)
    (hinv : GlobalInv p st) (hsi : si < p.ring) (hlt : ∀ pc ∈ active, pc < p.n) (hlen : active.length ≤ p.n)
    (hpos : st.pos ≤ input.bytes.size) :
    let r := boundaryBody p atoms input st si prev active
    GlobalInv p r.1 ∧
    stepFigure r.1.m p.k p.ring atom ≤ stepFigure st.m p.k p.ring atom + bodyCost p atom ∧
    st.pos ≤ r.1.pos ∧ r.1.pos ≤ input.bytes.size ∧ (r.2.isSome → st.pos < r.1.pos) := by
  intro r
  have hn : 1 ≤ p.n := by have := hwf.1; omega
  -- The flags, the queue copy, the spawn, and the closure.
  have h1 : GlobalInv p (st.setFlags input prev) := hinv.of_same _ rfl rfl rfl rfl rfl rfl
  have h2 := copyQueue_step p _ active h1 hlt hlen
  obtain ⟨h3, hc3, hl3, hp3⟩ := spawn_step p _ si atom hwf h2 hsi (by simp [List.length_reverse]; omega)
  obtain ⟨h4, hc4, hp4⟩ := closeAt_step p _ si atom hwf h3 hsi hn
  have hqlen : (spawn p ((st.setFlags input prev).copyQueue active) si).queue.length ≤ p.n + 1 := by
    simp only [St.queue_copyQueue, List.length_reverse] at hl3; omega
  have hclose : stepFigure (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)).m p.k p.ring atom ≤
      stepFigure st.m p.k p.ring atom + (2 * p.k + 7) + weight p.k * (p.n + 1) * (p.n + 1) := by
    rw [stepFigure_copyQueue, St.m_setFlags] at hc3
    have hw : weight p.k * ((spawn p ((st.setFlags input prev).copyQueue active) si).queue.length + p.n * (p.n + 1)) ≤
        weight p.k * (p.n + 1) * (p.n + 1) := by
      rw [Nat.mul_assoc]
      apply Nat.mul_le_mul_left
      have : (p.n + 1) * (p.n + 1) = (p.n + 1) + p.n * (p.n + 1) := by
        rw [Nat.succ_mul]; omega
      omega
    omega
  have hposc : (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)).pos = st.pos := by
    rw [hp4, hp3, St.pos_copyQueue, St.pos_setFlags]
  simp only [r, boundaryBody]
  split
  · rename_i hend
    simp only [beq_iff_eq] at hend
    refine ⟨h4, ?_, by rw [hposc]; exact Nat.le_refl _, by rw [hposc]; exact hpos, by simp⟩
    dsimp only
    unfold bodyCost; omega
  · rename_i hend
    simp only [beq_iff_eq] at hend
    have hlt' : st.pos < input.bytes.size := by omega
    obtain ⟨hs1, hs2⟩ := decodeRuneAt_size input.bytes st.pos hlt'
    obtain ⟨h5, hc5, hq5⟩ := paConsume_step p atoms input si hwf hawf hring atom _ h4 hsi
    have hposk : (paConsume p atoms input (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)) si).pos = st.pos := by
      rw [paConsume_pos, hposc]
    obtain ⟨h6, hc6, hp6, hsome6⟩ := afterConsume_step p _ (decodeRuneAt input.bytes st.pos).2 atom h5
    refine ⟨h6, ?_, ?_, ?_, ?_⟩
    · unfold bodyCost; omega
    · rw [hposk] at hp6; rcases hp6 with h | h <;> omega
    · rw [hposk] at hp6; rcases hp6 with h | h <;> omega
    · intro hs
      have := hsome6 hs
      rw [hposk] at this
      omega

theorem bumpSkipped_step (p : Prog) (st : St) (sc : Nat) (atom : Nat) (hinv : GlobalInv p st) :
    GlobalInv p (st.bumpSkipped sc) ∧
    stepFigure (st.bumpSkipped sc).m p.k p.ring atom = stepFigure st.m p.k p.ring atom + sc := by
  refine ⟨hinv.of_same _ rfl rfl rfl rfl rfl rfl, ?_⟩
  simp only [stepFigure, St.bumpSkipped]; omega

theorem boundaryAfterFilter_step (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int)
    (active : List Nat) (si : Nat) (atom : Nat) (hwf : p.wf) (hawf : atoms.wf p) (hring : 2 ≤ p.ring)
    (hinv : GlobalInv p st) (hsi : si < p.ring) (hlt : ∀ pc ∈ active, pc < p.n) (hlen : active.length ≤ p.n)
    (hpos : st.pos ≤ input.bytes.size) :
    let r := boundaryAfterFilter p atoms input st prev active si
    GlobalInv p r.1 ∧
    stepFigure r.1.m p.k p.ring atom ≤ stepFigure st.m p.k p.ring atom + bodyCost p atom + 1 + (r.1.pos - st.pos) ∧
    st.pos ≤ r.1.pos ∧ r.1.pos ≤ input.bytes.size ∧ (r.2.isSome → st.pos < r.1.pos) := by
  intro r
  simp only [r, boundaryAfterFilter]
  split
  · obtain ⟨hs1, hs2, hs3⟩ := scanAhead_spec p input st.pos hpos
    split
    · rename_i hnext
      obtain ⟨hg, hc⟩ := bumpSkipped_step p st (scanAhead p input st.pos).2 atom hinv
      refine ⟨hg.of_same _ rfl rfl rfl rfl rfl rfl, ?_, ?_, ?_, fun _ => ?_⟩
      · simp only [St.m_jumpTo, hc, St.pos_jumpTo]
        omega
      · simp only [St.pos_jumpTo]; omega
      · simp only [St.pos_jumpTo]; omega
      · simp only [St.pos_jumpTo]; exact hnext
    · rename_i hnext
      obtain ⟨hg, hc⟩ := bumpSkipped_step p st (scanAhead p input st.pos).2 atom hinv
      obtain ⟨h1, h2, h3, h4, h5⟩ := boundaryBody_step p atoms input _ si prev active atom hwf hawf hring hg hsi hlt hlen
        (by simpa using hpos)
      refine ⟨h1, ?_, by simpa using h3, h4, by simpa using h5⟩
      simp only [St.pos_bumpSkipped] at h2 h3 ⊢
      have hsc : (scanAhead p input st.pos).2 ≤ 1 := by omega
      omega
  · obtain ⟨h1, h2, h3, h4, h5⟩ := boundaryBody_step p atoms input st si prev active atom hwf hawf hring hinv hsi hlt hlen hpos
    exact ⟨h1, by omega, h3, h4, h5⟩

theorem liveAt_lt (p : Prog) (st : St) (si g : Nat) (hsl : SlotInv p (st.slot si)) :
    ∀ pc ∈ liveAt st si g, pc < p.n := by
  intro pc hpc
  unfold liveAt at hpc
  rw [List.mem_filter] at hpc
  exact (hsl.mem pc hpc.1).1

theorem liveAt_len (p : Prog) (st : St) (si g : Nat) (hsl : SlotInv p (st.slot si)) :
    (liveAt st si g).length ≤ (st.slot si).active.length :=
  List.length_filter_le _ _

theorem filterSlot_step (p : Prog) (st : St) (si g : Nat) (atom : Nat) (hinv : GlobalInv p st) (hsi : si < p.ring) :
    GlobalInv p (st.filterSlot si g) ∧
    stepFigure (st.filterSlot si g).m p.k p.ring atom ≤ stepFigure st.m p.k p.ring atom + p.n := by
  have hsl := hinv.slots si hsi
  have hslot' : (st.filterSlot si g).slot si = { st.slot si with gen := g, active := liveAt st si g } := by
    simp only [St.filterSlot, St.slot, St.setSlot]
    simp [Array.getElem?_setIfInBounds, hinv.slot_lt hsi]
  have hother : ∀ sj, si ≠ sj → (st.filterSlot si g).slot sj = st.slot sj := by
    intro sj hne
    simp only [St.filterSlot, St.slot, St.setSlot]
    simp [Array.getElem?_setIfInBounds, hne]
  have hnew : SlotInv p { st.slot si with gen := g, active := liveAt st si g } :=
    ⟨hsl.size, List.Pairwise.filter _ hsl.nodup, fun pc hpc => by
      unfold liveAt at hpc
      rw [List.mem_filter] at hpc
      refine ⟨(hsl.mem pc hpc.1).1, ?_⟩
      have h2 := hpc.2
      simp only [beq_iff_eq, Slot.entry_eq] at h2
      simp only [Slot.entry_eq]
      exact h2, hsl.cap⟩
  have hcap := capSum_setSlot p st si { st.slot si with gen := g, active := liveAt st si g } hsi hinv.slotsSize
  refine ⟨⟨by rw [St.slots_filterSlot]; exact hinv.slotsSize, ?_, by simpa using hinv.queueLt,
    by simpa using hinv.queueLen, hinv.queueCap, hinv.aheadLen, hinv.aheadCap, ?_⟩, ?_⟩
  · intro sj hsj
    by_cases h : si = sj
    · subst h; rw [hslot']; exact hnew
    · rw [hother sj h]; exact hinv.slots sj hsj
  · have halloc := hinv.alloc
    have hcs : capSum p (st.filterSlot si g) = capSum p (st.setSlot si { st.slot si with gen := g, active := liveAt st si g }) := by
      simp [capSum, St.filterSlot, St.slot, St.setSlot]
    rw [hcs]
    have hm : (st.filterSlot si g).m.allocBytes = st.m.allocBytes := rfl
    rw [hm]
    have h2 : ({ st.slot si with gen := g, active := liveAt st si g } : Slot).activeCap = (st.slot si).activeCap := rfl
    rw [h2] at hcap
    omega
  · have hl := hsl.active_len
    simp only [stepFigure, St.filterSlot]
    omega

theorem boundaryStep_step (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int) (atom : Nat)
    (hwf : p.wf) (hawf : atoms.wf p) (hring : 2 ≤ p.ring) (hinv : GlobalInv p st) (hpos : st.pos ≤ input.bytes.size) :
    let r := boundaryStep p atoms input st prev
    GlobalInv p r.1 ∧
    stepFigure r.1.m p.k p.ring atom ≤ stepFigure st.m p.k p.ring atom + perBoundary p atom + (r.1.pos - st.pos) ∧
    st.pos ≤ r.1.pos ∧ r.1.pos ≤ input.bytes.size ∧ (r.2.isSome → st.pos < r.1.pos) := by
  intro r
  simp only [r, boundaryStep]
  have hsi : st.ci % p.ring < p.ring := Nat.mod_lt _ (by omega)
  have hb : GlobalInv p st.bumpBoundaries := hinv.of_same _ rfl rfl rfl rfl rfl rfl
  have hbc : stepFigure st.bumpBoundaries.m p.k p.ring atom = stepFigure st.m p.k p.ring atom + (14 + 2 * p.ring) := by
    simp only [stepFigure, St.bumpBoundaries, Nat.add_mul, Nat.one_mul]; omega
  obtain ⟨hf, hfc⟩ := filterSlot_step p st.bumpBoundaries (st.ci % p.ring) (paGen st.ci) atom hb hsi
  have hsl := hinv.slots _ hsi
  have hlt := liveAt_lt p st _ (paGen st.ci) hsl
  have hlen : (liveAt st (st.ci % p.ring) (paGen st.ci)).length ≤ p.n :=
    Nat.le_trans (liveAt_len p st _ _ hsl) hsl.active_len
  obtain ⟨h1, h2, h3, h4, h5⟩ := boundaryAfterFilter_step p atoms input _ prev _ _ atom hwf hawf hring hf hsi hlt hlen
    (by simpa using hpos)
  simp only [St.pos_filterSlot, St.pos_bumpBoundaries] at h2 h3 h5
  refine ⟨h1, ?_, h3, h4, h5⟩
  unfold perBoundary bodyCost at *
  omega

theorem paRun_bound (p : Prog) (atoms : Atoms) (input : Input) (atom : Nat) (hwf : p.wf) (hawf : atoms.wf p)
    (hring : 2 ≤ p.ring) :
    ∀ (fuel : Nat) (st : St) (prev : Int), GlobalInv p st → st.pos ≤ input.bytes.size →
      GlobalInv p (paRun p atoms input st prev fuel) ∧
      stepFigure (paRun p atoms input st prev fuel).m p.k p.ring atom ≤
        stepFigure st.m p.k p.ring atom + (input.bytes.size - st.pos + 1) * perBoundary p atom +
          (input.bytes.size - st.pos) := by
  intro fuel
  induction fuel with
  | zero => intro st prev h _; exact ⟨h, by simp only [paRun]; omega⟩
  | succ fuel ih =>
    intro st prev hinv hpos
    obtain ⟨h1, h2, h3, h4, h5⟩ := boundaryStep_step p atoms input st prev atom hwf hawf hring hinv hpos
    simp only [paRun]
    split
    · rename_i st' hst
      have hp : (boundaryStep p atoms input st prev).1 = st' := by rw [hst]
      rw [hp] at h1 h2 h3 h4
      refine ⟨h1, ?_⟩
      have : (input.bytes.size - st.pos + 1) * perBoundary p atom ≥ perBoundary p atom := by
        have : 1 ≤ input.bytes.size - st.pos + 1 := by omega
        calc perBoundary p atom = 1 * perBoundary p atom := (Nat.one_mul _).symm
          _ ≤ _ := Nat.mul_le_mul_right _ this
      omega
    · rename_i st' prev' hst
      have hp : (boundaryStep p atoms input st prev).1 = st' := by rw [hst]
      have hsome : (boundaryStep p atoms input st prev).2.isSome := by rw [hst]; rfl
      have hlt := h5 hsome
      rw [hp] at h1 h2 h3 h4 hlt
      obtain ⟨hg, hc⟩ := ih st' prev' h1 h4
      refine ⟨hg, ?_⟩
      have hsplit : (input.bytes.size - st.pos + 1) * perBoundary p atom =
          (input.bytes.size - st'.pos + 1) * perBoundary p atom + (st'.pos - st.pos) * perBoundary p atom := by
        rw [← Nat.add_mul]
        congr 1
        omega
      have hone : perBoundary p atom ≤ (st'.pos - st.pos) * perBoundary p atom := by
        have : 1 ≤ st'.pos - st.pos := by omega
        calc perBoundary p atom = 1 * perBoundary p atom := (Nat.one_mul _).symm
          _ ≤ _ := Nat.mul_le_mul_right _ this
      omega

/-! ## The workspace at the start, and the final bounds -/

theorem prepare_slot (p : Prog) (si : Nat) (hsi : si < p.ring) :
    (prepare p).slot si = { table := Array.replicate p.n { ctr := List.replicate p.k 0 }, active := [], activeCap := 0, gen := 0 } := by
  simp [prepare, St.slot, hsi]

theorem sum_range_le_const (f : Nat → Nat) (n c : Nat) (h : ∀ i, i < n → f i ≤ c) :
    ((List.range n).map f).sum ≤ n * c := by
  induction n with
  | zero => simp
  | succ n ih =>
    rw [List.range_succ, List.map_append, List.sum_append]
    simp only [List.map_cons, List.map_nil, List.sum_cons, List.sum_nil, Nat.add_zero]
    have := ih fun i hi => h i (by omega)
    have := h n (by omega)
    rw [Nat.succ_mul]
    omega

theorem prepare_globalInv (p : Prog) : GlobalInv p (prepare p) := by
  refine ⟨by simp [prepare], ?_, by simp [prepare], by simp [prepare], by simp [prepare], by simp [prepare],
    by simp [prepare], ?_⟩
  · intro si hsi
    rw [prepare_slot p si hsi]
    exact ⟨by simp, List.nodup_nil, fun pc hpc => by simp at hpc, by simp⟩
  · have h1 : ((List.range p.ring).map fun si => ((prepare p).slot si).activeCap).sum ≤ p.ring * 0 :=
      sum_range_le_const _ _ _ (fun si hsi => by rw [prepare_slot p si hsi]; exact Nat.le_refl _)
    have h2 : (prepare p).queueCap = 16 := rfl
    have h3 : (prepare p).aheadCap = 0 := rfl
    have h4 : (prepare p).m.allocBytes = prepareBytes p.n p.k p.ring := rfl
    unfold capSum
    rw [h2, h3, h4]
    omega

theorem prepare_stepFigure (p : Prog) (atom : Nat) : stepFigure (prepare p).m p.k p.ring atom = 24 + p.ring := by
  simp [stepFigure, prepare]

/-- Phase A never exceeds its step figure: for every well-formed program, every atom test, and every subject. -/
theorem run_steps_le (p : Prog) (atoms : Atoms) (input : Input) (atom : Nat) (hwf : p.wf) (hawf : atoms.wf p)
    (hring : 2 ≤ p.ring) :
    stepFigure (run p atoms input).m p.k p.ring atom ≤ stepsFigure p atom input.bytes.size := by
  unfold run stepsFigure
  obtain ⟨_, hc⟩ := paRun_bound p atoms input atom hwf hawf hring (input.bytes.size + 2) (prepare p) (-2)
    (prepare_globalInv p) (by simp [prepare])
  rw [prepare_stepFigure] at hc
  have hp0 : (prepare p).pos = 0 := rfl
  rw [hp0] at hc
  simp only [Nat.sub_zero] at hc
  refine Nat.le_trans hc ?_
  omega

/-- Phase A never exceeds its heap figure. -/
theorem run_heap_le (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (hawf : atoms.wf p)
    (hring : 2 ≤ p.ring) :
    (run p atoms input).m.allocBytes ≤ heapFigure p.n p.k p.ring := by
  show (paRun p atoms input (prepare p) (-2) (input.bytes.size + 2)).m.allocBytes ≤ _
  unfold heapFigure
  obtain ⟨hg, _⟩ := paRun_bound p atoms input 0 hwf hawf hring (input.bytes.size + 2) (prepare p) (-2)
    (prepare_globalInv p) (by simp [prepare])
  have halloc := hg.alloc
  have hcaps : capSum p (paRun p atoms input (prepare p) (-2) (input.bytes.size + 2)) ≤
      p.ring * (2 * p.n + 8) + (4 * p.n + 26) + 24 := by
    unfold capSum
    have := sum_range_le_const (fun si => ((paRun p atoms input (prepare p) (-2) (input.bytes.size + 2)).slot si).activeCap)
      p.ring (2 * p.n + 8) (fun si hsi => (hg.slots si hsi).cap)
    have := hg.queueCap
    have := hg.aheadCap
    omega
  have h8 := Nat.mul_le_mul_left 8 hcaps
  rw [Nat.mul_add, Nat.mul_add] at h8
  have hr : p.ring * (16 * p.n + 64) = 8 * (p.ring * (2 * p.n + 8)) := by
    rw [Nat.mul_left_comm]; exact congrArg _ (by omega)
  omega

end PhaseA
end Vego
