import Vego.PhaseACorrect

/-!
# Phase A: the run-level correctness proof

`PhaseACorrect.lean` proves the closure of one boundary correct. This file takes the model through the
consuming phase, the advance, the early stop and the scan filter, and puts the boundaries together into the
statement about `run`: the reported match is the reference's earliest-start, minimal-counters, longest-end
candidate, and there is none when the run reports no match.
-/

namespace Vego
namespace PhaseA

/-! ## Arithmetic on the ring -/

/-- Two offsets below the ring size that land on the same slot are equal. -/
theorem mod_add_eq (ci d delta r : Nat) (_ : 0 < r) (hd : d < r) (hdel : delta < r)
    (h : (ci + d) % r = (ci + delta) % r) : d = delta := by
  have h1 := Nat.div_add_mod (ci + d) r
  have h2 := Nat.div_add_mod (ci + delta) r
  rcases Nat.lt_trichotomy ((ci + d) / r) ((ci + delta) / r) with hlt | heq | hgt
  · have := Nat.mul_le_mul_left r hlt
    rw [Nat.mul_succ] at this
    omega
  · rw [heq] at h1; omega
  · have := Nat.mul_le_mul_left r hgt
    rw [Nat.mul_succ] at this
    omega

theorem Op.beq_iff (a b : Op) : (a == b) = true ↔ a = b := by
  cases a <;> cases b <;> decide

theorem slot_of_slots (st st' : St) (h : st'.slots = st.slots) (si : Nat) : st'.slot si = st.slot si := by
  simp [St.slot, h]

/-! ## The lookahead the model decodes is the reference lookahead -/

theorem pushAhead_fields (st : St) (r : Int) :
    (st.pushAhead r).ahead = st.ahead ++ [r] ∧ (st.pushAhead r).slots = st.slots ∧ (st.pushAhead r).ci = st.ci ∧
    (st.pushAhead r).pos = st.pos ∧ (st.pushAhead r).cur = st.cur ∧ bestOf (st.pushAhead r) = bestOf st ∧
    (st.pushAhead r).matched = st.matched := by
  simp [St.pushAhead, St.charge, bestOf]

theorem decodeAheadFrom_fields (input : Input) : ∀ (fuel at_ : Nat) (st : St),
    (decodeAheadFrom input.bytes at_ st fuel).ahead = st.ahead ++ aheadList input at_ fuel ∧
    (decodeAheadFrom input.bytes at_ st fuel).slots = st.slots ∧
    (decodeAheadFrom input.bytes at_ st fuel).ci = st.ci ∧
    (decodeAheadFrom input.bytes at_ st fuel).pos = st.pos ∧
    (decodeAheadFrom input.bytes at_ st fuel).cur = st.cur ∧
    bestOf (decodeAheadFrom input.bytes at_ st fuel) = bestOf st ∧
    (decodeAheadFrom input.bytes at_ st fuel).matched = st.matched := by
  intro fuel
  induction fuel with
  | zero => intro at_ st; simp [decodeAheadFrom, aheadList]
  | succ fuel ih =>
    intro at_ st
    simp only [decodeAheadFrom, aheadList]
    by_cases h : at_ < input.bytes.size
    · rw [if_pos h, if_pos h]
      have hstep : stepPos input at_ = at_ + (decodeRuneAt input.bytes at_).2 := by
        unfold stepPos sizeAt; rw [if_neg (by simp; omega)]
      rw [hstep]
      generalize hdec : decodeRuneAt input.bytes at_ = rs
      obtain ⟨r, size⟩ := rs
      dsimp only
      obtain ⟨h1, h2, h3, h4, h5, h6, h7⟩ := ih (at_ + size) (st.pushAhead r)
      obtain ⟨p1, p2, p3, p4, p5, p6, p7⟩ := pushAhead_fields st r
      refine ⟨?_, h2.trans p2, h3.trans p3, h4.trans p4, h5.trans p5, h6.trans p6, h7.trans p7⟩
      rw [h1, p1, List.append_assoc]; rfl
    · rw [if_neg h, if_neg h]; simp

theorem decodeAhead_fields (input : Input) (st : St) :
    (decodeAhead input st).ahead = aheadAt input st.pos ∧ (decodeAhead input st).slots = st.slots ∧
    (decodeAhead input st).ci = st.ci ∧ (decodeAhead input st).pos = st.pos ∧ (decodeAhead input st).cur = st.cur ∧
    bestOf (decodeAhead input st) = bestOf st ∧ (decodeAhead input st).matched = st.matched := by
  obtain ⟨h1, h2, h3, h4, h5, h6, h7⟩ := decodeAheadFrom_fields input maxElemAhead st.pos st.resetAhead
  unfold decodeAhead
  refine ⟨?_, h2, h3, h4, h5, h6, h7⟩
  rw [h1]; simp [St.resetAhead, aheadAt]

/-! ## Coverage of an arrival at a future slot -/

/-- An arrival at instruction `r` of slot `sj` is covered: the slot is at generation `g` and holds a payload at
least as good, or the payload cannot beat the best match. -/
def SlotCov (p : Prog) (st : St) (g sj r : Nat) (v : Payload) : Prop :=
  ((st.slot sj).gen = g ∧ Holds p.k (st.slot sj) r v) ∨ Dom p.k st v

theorem SlotCov_of_bestOf (p : Prog) (st st' : St) (hb : bestOf st' = bestOf st) (hm : st'.matched = st.matched)
    (hslots : st'.slots = st.slots) (g sj r : Nat) (v : Payload) (h : SlotCov p st g sj r v) :
    SlotCov p st' g sj r v := by
  rcases h with ⟨hg, hh⟩ | hd
  · left; rw [slot_of_slots st st' hslots]; exact ⟨hg, hh⟩
  · right; exact Dom_of_bestOf p.k st st' hb hm v hd

/-- Coverage persists through an arrival. -/
theorem SlotCov_arrive (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (st : St) (pc delta start : Nat)
    (ctr : List Nat) (hring : 2 ≤ p.ring) (hsize : st.slots.size = p.ring) (hd : 1 ≤ delta) (hd2 : delta < p.ring)
    (hpc : pc < p.n) (hl : ctr.length = p.k)
    (hrs : RSlotQ p (st.slot ((st.ci + delta) % p.ring)) (paGen (st.ci + delta)))
    (d r : Nat) (v : Payload) (hdr : d < p.ring) (hr : r < p.n)
    (h : SlotCov p st (paGen (st.ci + d)) ((st.ci + d) % p.ring) r v) :
    SlotCov p (paArrive p st pc delta start ctr) (paGen (st.ci + d)) ((st.ci + d) % p.ring) r v := by
  obtain ⟨hci, hpos, hcur, hsz, hbest, hm, hah, hother, hgen', hrs', hcov, hpers, hfresh⟩ :=
    paArrive_effect p atoms input hwf st pc delta start ctr hring hsize hd hd2 hpc hl hrs
  rcases h with ⟨hg, hh⟩ | hdom
  · left
    by_cases heq : (st.ci + d) % p.ring = (st.ci + delta) % p.ring
    · have : d = delta := mod_add_eq st.ci d delta p.ring (by omega) hdr hd2 heq
      subst this
      exact ⟨hgen', hpers r v hr hg hh⟩
    · rw [hother _ heq]; exact ⟨hg, hh⟩
  · right; exact Dom_of_bestOf p.k st _ hbest hm v hdom

/-! ## The consuming phase -/

/-- What the consuming phase knows about the closed state `st3` it starts from. -/
structure ConsCtx (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 : St) : Prop where
  ciEq : st3.ci = ci
  wf : p.wf
  awf : atoms.wf2 p
  ring : 2 ≤ p.ring
  size : st3.slots.size = p.ring
  pos : st3.pos = chainPos input idx
  posLt : chainPos input idx < input.bytes.size
  cur : st3.cur = curAt input (chainPos input idx)
  rslot : RSlot p (st3.slot (ci % p.ring))
  sound : ∀ pc, pc < p.n → Fresh (st3.slot (ci % p.ring)) pc →
    Reach p atoms input (thOf idx (st3.slot (ci % p.ring)) pc)

/-- The payload an arrival from the entry at `pc` of the current slot delivers `delta` characters ahead. -/
def arrPayload (p : Prog) (ci : Nat) (st3 : St) (pc delta : Nat) : Payload :=
  (((st3.slot (ci % p.ring)).entry pc).start, bumpIf p pc ((st3.slot (ci % p.ring)).entry pc).ctr delta)

/-- The invariant of the consuming phase: `st` is the state after the instructions of `done` were consumed. -/
structure ConsInv (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st : St) (ready : Bool)
    (done : List Nat) : Prop where
  ciEq : st.ci = ci
  pos : st.pos = st3.pos
  cur : st.cur = st3.cur
  size : st.slots.size = st3.slots.size
  best : bestOf st = bestOf st3
  matched : st.matched = st3.matched
  self : st.slot (ci % p.ring) = st3.slot (ci % p.ring)
  ahead : ready = true → st.ahead = aheadAt input (chainPos input idx)
  rslot : ∀ d, 1 ≤ d → d < p.ring → RSlotQ p (st.slot ((ci + d) % p.ring)) (paGen (ci + d))
  sound : ∀ d, 1 ≤ d → d < p.ring → (st.slot ((ci + d) % p.ring)).gen = paGen (ci + d) →
    ∀ pc, pc < p.n → Fresh (st.slot ((ci + d) % p.ring)) pc →
      Arrived p atoms input (idx + 1) (thOf (idx + d) (st.slot ((ci + d) % p.ring)) pc)
  persist : ∀ d r w, 1 ≤ d → d < p.ring → r < p.n →
    SlotCov p st3 (paGen (ci + d)) ((ci + d) % p.ring) r w →
    SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r w
  arrivals : ∀ pc ∈ done, pc < p.n → Fresh (st3.slot (ci % p.ring)) pc → ∀ delta,
    Consumes p atoms input idx pc delta →
    SlotCov p st (paGen (ci + delta)) ((ci + delta) % p.ring) (p.next pc) (arrPayload p ci st3 pc delta)

/-- A step of the consuming phase that leaves the slots alone keeps the invariant. -/
theorem ConsInv_same (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st st' : St) (ready ready' : Bool)
    (done : List Nat) (h : ConsInv p atoms input idx ci st3 st ready done)
    (hslots : st'.slots = st.slots) (hci : st'.ci = st.ci) (hpos : st'.pos = st.pos) (hcur : st'.cur = st.cur)
    (hbest : bestOf st' = bestOf st) (hm : st'.matched = st.matched)
    (hahead : ready' = true → st'.ahead = aheadAt input (chainPos input idx)) :
    ConsInv p atoms input idx ci st3 st' ready' done := by
  have hs : ∀ si, st'.slot si = st.slot si := slot_of_slots st st' hslots
  refine ⟨hci.trans h.ciEq, hpos.trans h.pos, hcur.trans h.cur, by rw [hslots]; exact h.size, hbest.trans h.best,
    hm.trans h.matched, by rw [hs]; exact h.self, hahead, fun d h1 h2 => by rw [hs]; exact h.rslot d h1 h2,
    fun d h1 h2 hg pc hpc hf => by
      rw [hs] at hg hf ⊢; exact h.sound d h1 h2 hg pc hpc hf,
    fun d r w h1 h2 h3 hc => SlotCov_of_bestOf p st st' hbest hm hslots _ _ _ _ (h.persist d r w h1 h2 h3 hc),
    fun pc hpc hn hf delta hc => SlotCov_of_bestOf p st st' hbest hm hslots _ _ _ _ (h.arrivals pc hpc hn hf delta hc)⟩

/-- A consuming edge advances by at least one character and by less than the ring. -/
theorem Consumes_delta (p : Prog) (atoms : Atoms) (input : Input) (hawf : atoms.wf2 p) (hring : 2 ≤ p.ring)
    (idx pc delta : Nat) (hcons : Consumes p atoms input idx pc delta) : 1 ≤ delta ∧ delta < p.ring := by
  obtain ⟨_, hc⟩ := hcons
  rcases hc with ⟨hd, _, _⟩ | ⟨_, hd, _, _⟩
  · omega
  · have := hawf pc delta hd; omega

/-- One arrival from a fresh entry of the current slot along a consuming edge of the reference. -/
theorem ConsInv_arrive (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st : St) (ready : Bool)
    (done : List Nat) (ctx : ConsCtx p atoms input idx ci st3) (h : ConsInv p atoms input idx ci st3 st ready done)
    (pc delta : Nat) (hpc : pc < p.n) (hf : Fresh (st3.slot (ci % p.ring)) pc)
    (hcons : Consumes p atoms input idx pc delta) :
    let e := (st3.slot (ci % p.ring)).entry pc
    let st' := paArrive p st pc delta e.start e.ctr
    ConsInv p atoms input idx ci st3 st' ready done ∧
    SlotCov p st' (paGen (ci + delta)) ((ci + delta) % p.ring) (p.next pc) (arrPayload p ci st3 pc delta) ∧
    (∀ d r w, 1 ≤ d → d < p.ring → r < p.n →
      SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r w →
      SlotCov p st' (paGen (ci + d)) ((ci + d) % p.ring) r w) := by
  intro e st'
  have hd := Consumes_delta p atoms input ctx.awf ctx.ring idx pc delta hcons
  have hsize : st.slots.size = p.ring := h.size.trans ctx.size
  have hl : e.ctr.length = p.k := ctx.rslot.lens pc hpc
  have hci0 : st.ci = ci := h.ciEq
  have hrs := h.rslot delta hd.1 hd.2
  rw [← hci0] at hrs
  obtain ⟨hci, hpos, hcur, hsz, hbest, hm, hah, hother, hgen', hrs', hcov, hpers, hfresh⟩ :=
    paArrive_effect p atoms input ctx.wf st pc delta e.start e.ctr ctx.ring hsize hd.1 hd.2 hpc hl hrs
  have hpersist : ∀ d r w, 1 ≤ d → d < p.ring → r < p.n →
      SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r w →
      SlotCov p st' (paGen (ci + d)) ((ci + d) % p.ring) r w := by
    intro d r w h1 h2 h3 hc
    rw [← hci0] at hc ⊢
    exact SlotCov_arrive p atoms input ctx.wf st pc delta e.start e.ctr ctx.ring hsize hd.1 hd.2 hpc hl hrs
      d r w h2 h3 hc
  rw [hci0] at hother hgen' hrs' hcov hpers hfresh
  have hne : (ci + delta) % p.ring ≠ ci % p.ring := mod_add_ne ci delta p.ring ctx.ring hd.1 hd.2
  refine ⟨⟨hci.trans h.ciEq, hpos.trans h.pos, hcur.trans h.cur, hsz.trans h.size, hbest.trans h.best,
    hm.trans h.matched, by rw [hother _ (Ne.symm hne)]; exact h.self, fun hr => by rw [hah]; exact h.ahead hr,
    ?_, ?_, fun d r w h1 h2 h3 hc => hpersist d r w h1 h2 h3 (h.persist d r w h1 h2 h3 hc),
    fun q hq hqn hfq d hc =>
      have hd' := Consumes_delta p atoms input ctx.awf ctx.ring idx q d hc
      hpersist _ _ _ hd'.1 hd'.2 (ctx.wf.2 q hqn).1 (h.arrivals q hq hqn hfq d hc)⟩,
    ?_, hpersist⟩
  · intro d h1 h2
    by_cases heq : (ci + d) % p.ring = (ci + delta) % p.ring
    · have := mod_add_eq ci d delta p.ring (by omega) h2 hd.2 heq
      subst this; exact RSlotQ.ofRSlot p _ _ hrs' (by rw [hgen']; exact Nat.le_refl _)
    · rw [hother _ heq]; exact h.rslot d h1 h2
  · intro d h1 h2 hg q hq hfq
    by_cases heq : (ci + d) % p.ring = (ci + delta) % p.ring
    · have := mod_add_eq ci d delta p.ring (by omega) h2 hd.2 heq
      subst this
      rcases hfresh q hq hfq with ⟨hent, hold⟩ | ⟨rfl, hent⟩
      · have hf' : Fresh (st.slot ((ci + d) % p.ring)) q := by
          unfold Fresh at hfq ⊢; rw [hent, hgen'] at hfq; rw [hfq, hold]
        have := h.sound d h1 h2 hold q hq hf'
        unfold thOf at this ⊢; rw [hent]; exact this
      · have hr := ctx.sound pc hpc hf
        unfold thOf at hr ⊢
        rw [hent]
        exact ⟨⟨idx, pc, e.start, e.ctr⟩, d, hr, Nat.lt_succ_self idx, hcons, rfl⟩
    · rw [hother _ heq] at hg hfq ⊢; exact h.sound d h1 h2 hg q hq hfq
  · rcases hcov with hh | hdom
    · left; exact ⟨hgen', hh⟩
    · right; exact Dom_of_bestOf p.k st st' hbest hm _ hdom

theorem ConsInv_bumpTests (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st : St) (ready : Bool)
    (done : List Nat) (h : ConsInv p atoms input idx ci st3 st ready done) :
    ConsInv p atoms input idx ci st3 st.bumpTests ready done :=
  ConsInv_same p atoms input idx ci st3 st st.bumpTests ready ready done h rfl rfl rfl rfl rfl rfl h.ahead

theorem ConsInv_decodeAhead (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st : St) (ready : Bool)
    (done : List Nat) (ctx : ConsCtx p atoms input idx ci st3) (h : ConsInv p atoms input idx ci st3 st ready done) :
    ConsInv p atoms input idx ci st3 (decodeAhead input st) true done := by
  obtain ⟨h1, h2, h3, h4, h5, h6, h7⟩ := decodeAhead_fields input st
  exact ConsInv_same p atoms input idx ci st3 st _ ready true done h h2 h3 h4 h5 h6 h7
    (fun _ => by rw [h1, h.pos, ctx.pos])

/-- The single-character transition of one fresh instruction. -/
theorem consumeArrive_inv (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st : St) (ready : Bool)
    (done : List Nat) (ctx : ConsCtx p atoms input idx ci st3) (h : ConsInv p atoms input idx ci st3 st ready done)
    (pc : Nat) (hpc : pc < p.n) (hf : Fresh (st3.slot (ci % p.ring)) pc) (hop : (p.op pc).consuming = true) :
    let e := (st3.slot (ci % p.ring)).entry pc
    let st' := consumeArrive p atoms st pc e.start e.ctr
    ConsInv p atoms input idx ci st3 st' ready done ∧
    (Consumes p atoms input idx pc 1 →
      SlotCov p st' (paGen (ci + 1)) ((ci + 1) % p.ring) (p.next pc) (arrPayload p ci st3 pc 1)) ∧
    (∀ d r w, 1 ≤ d → d < p.ring → r < p.n →
      SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r w →
      SlotCov p st' (paGen (ci + d)) ((ci + d) % p.ring) r w) := by
  intro e st'
  have hcur : st.cur = curAt input (chainPos input idx) := h.cur.trans ctx.cur
  simp only [st', consumeArrive]
  by_cases hs : atoms.single pc st.cur = true
  · rw [if_pos hs]
    have hcons : Consumes p atoms input idx pc 1 := ⟨hop, Or.inl ⟨rfl, ctx.posLt, by rwa [hcur] at hs⟩⟩
    obtain ⟨inv, cov, pers⟩ := ConsInv_arrive p atoms input idx ci st3 st ready done ctx h pc 1 hpc hf hcons
    exact ⟨inv, fun _ => cov, pers⟩
  · rw [if_neg hs]
    refine ⟨h, fun hc => ?_, fun _ _ _ _ _ _ hc => hc⟩
    exfalso
    obtain ⟨_, hc⟩ := hc
    rcases hc with ⟨_, _, hsingle⟩ | ⟨_, hd, _, _⟩
    · rw [hcur] at hs; exact hs hsingle
    · have := ctx.awf pc 1 hd; omega

/-- The multi-character probes of a bracket over a sublist of its lengths. -/
theorem probeLens_inv (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 : St)
    (done : List Nat) (ctx : ConsCtx p atoms input idx ci st3) (pc : Nat) (hpc : pc < p.n)
    (hf : Fresh (st3.slot (ci % p.ring)) pc) (hop : p.op pc = .bracket) :
    ∀ (lens : List Nat) (st : St), ConsInv p atoms input idx ci st3 st true done → (∀ len ∈ lens, len ∈ atoms.lens pc) →
    let e := (st3.slot (ci % p.ring)).entry pc
    let st' := probeLens p atoms st pc e.start e.ctr lens
    ConsInv p atoms input idx ci st3 st' true done ∧
    (∀ len ∈ lens, Consumes p atoms input idx pc len →
      SlotCov p st' (paGen (ci + len)) ((ci + len) % p.ring) (p.next pc) (arrPayload p ci st3 pc len)) ∧
    (∀ d r w, 1 ≤ d → d < p.ring → r < p.n →
      SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r w →
      SlotCov p st' (paGen (ci + d)) ((ci + d) % p.ring) r w) := by
  intro lens
  induction lens with
  | nil =>
    intro st h _ e st'
    exact ⟨h, by simp, fun _ _ _ _ _ _ hc => hc⟩
  | cons len rest ih =>
    intro st h hlens e st'
    have hahead : st.ahead = aheadAt input (chainPos input idx) := h.ahead rfl
    have hrest : ∀ l ∈ rest, l ∈ atoms.lens pc := fun l hl => hlens l (List.mem_cons_of_mem _ hl)
    have hmem : len ∈ atoms.lens pc := hlens len (List.mem_cons_self ..)
    have hnext := (ctx.wf.2 pc hpc).1
    simp only [st', probeLens]
    by_cases hc : (len ≤ st.ahead.length && atoms.multi pc (List.take len st.ahead)) = true
    · rw [if_pos hc]
      simp only [Bool.and_eq_true, decide_eq_true_eq] at hc
      have hcons : Consumes p atoms input idx pc len :=
        ⟨by rw [hop]; rfl, Or.inr ⟨hop, hmem, by rw [← hahead]; exact hc.1, by rw [← hahead]; exact hc.2⟩⟩
      obtain ⟨inv1, cov1, pers1⟩ := ConsInv_arrive p atoms input idx ci st3 st true done ctx h pc len hpc hf hcons
      obtain ⟨inv2, cov2, pers2⟩ := ih _ inv1 hrest
      have hd := Consumes_delta p atoms input ctx.awf ctx.ring idx pc len hcons
      refine ⟨inv2, fun l hl hcl => ?_, fun d r w h1 h2 h3 hcv => pers2 d r w h1 h2 h3 (pers1 d r w h1 h2 h3 hcv)⟩
      rcases List.mem_cons.mp hl with rfl | hl
      · exact pers2 _ _ _ hd.1 hd.2 hnext cov1
      · exact cov2 l hl hcl
    · rw [if_neg hc]
      obtain ⟨inv2, cov2, pers2⟩ := ih _ h hrest
      refine ⟨inv2, fun l hl hcl => ?_, pers2⟩
      rcases List.mem_cons.mp hl with rfl | hl
      · exfalso
        obtain ⟨_, hcl⟩ := hcl
        rcases hcl with ⟨hd, _, _⟩ | ⟨_, _, hl1, hl2⟩
        · have := ctx.awf pc l hmem; omega
        · rw [← hahead] at hl1 hl2
          exact hc (by simp [hl1, hl2])
      · exact cov2 l hl hcl

/-- The probes of one instruction, decoding the lookahead when needed. -/
theorem consumeProbe_inv (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st : St) (ready : Bool)
    (done : List Nat) (ctx : ConsCtx p atoms input idx ci st3) (h : ConsInv p atoms input idx ci st3 st ready done)
    (pc : Nat) (hpc : pc < p.n) (hf : Fresh (st3.slot (ci % p.ring)) pc) :
    let e := (st3.slot (ci % p.ring)).entry pc
    let r := consumeProbe p atoms input st ready pc e.start e.ctr
    ConsInv p atoms input idx ci st3 r.1 r.2 done ∧
    (∀ delta, 2 ≤ delta → Consumes p atoms input idx pc delta →
      SlotCov p r.1 (paGen (ci + delta)) ((ci + delta) % p.ring) (p.next pc) (arrPayload p ci st3 pc delta)) ∧
    (∀ d r' w, 1 ≤ d → d < p.ring → r' < p.n →
      SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r' w →
      SlotCov p r.1 (paGen (ci + d)) ((ci + d) % p.ring) r' w) := by
  intro e r
  simp only [r, consumeProbe]
  by_cases hb : ((p.ins.getD pc default).op == Op.bracket && !(atoms.lens pc).isEmpty) = true
  · rw [if_pos hb]
    simp only [Bool.and_eq_true, Op.beq_iff, Bool.not_eq_true', List.isEmpty_eq_false_iff] at hb
    have hop : p.op pc = .bracket := hb.1
    -- The state the probes start from has the lookahead decoded.
    have hstart : ∃ st1 : St, (if ready = true then st else decodeAhead input st) = st1 ∧
        ConsInv p atoms input idx ci st3 st1 true done ∧
        (∀ d r' w, 1 ≤ d → d < p.ring → r' < p.n →
          SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r' w →
          SlotCov p st1 (paGen (ci + d)) ((ci + d) % p.ring) r' w) := by
      by_cases hr : ready = true
      · rw [if_pos hr]
        exact ⟨st, rfl, ConsInv_same p atoms input idx ci st3 st st ready true done h rfl rfl rfl rfl rfl rfl
          (fun _ => h.ahead hr), fun _ _ _ _ _ _ hc => hc⟩
      · rw [if_neg hr]
        obtain ⟨_, h2, _, _, _, h6, h7⟩ := decodeAhead_fields input st
        exact ⟨_, rfl, ConsInv_decodeAhead p atoms input idx ci st3 st ready done ctx h,
          fun d r' w _ _ _ hc => SlotCov_of_bestOf p st _ h6 h7 h2 _ _ _ _ hc⟩
    obtain ⟨st1, hst1, inv1, pers1⟩ := hstart
    rw [hst1]
    obtain ⟨inv2, cov2, pers2⟩ := probeLens_inv p atoms input idx ci st3 done ctx pc hpc hf hop (atoms.lens pc) st1 inv1
      (fun _ hl => hl)
    refine ⟨inv2, fun delta h2 hc => ?_, fun d r' w h1 h2 h3 hc => pers2 d r' w h1 h2 h3 (pers1 d r' w h1 h2 h3 hc)⟩
    have hc' := hc.2
    rcases hc' with ⟨hd, _, _⟩ | ⟨_, hd, _, _⟩
    · omega
    · exact cov2 delta hd hc
  · rw [if_neg hb]
    refine ⟨h, fun delta h2 hc => ?_, fun _ _ _ _ _ _ hc => hc⟩
    exfalso
    obtain ⟨_, hc⟩ := hc
    rcases hc with ⟨hd, _, _⟩ | ⟨hop, hd, _, _⟩
    · omega
    · apply hb
      simp only [Bool.and_eq_true, Op.beq_iff, Bool.not_eq_true', List.isEmpty_eq_false_iff]
      exact ⟨hop, List.ne_nil_of_mem hd⟩

/-- One fresh instruction: its single test and its probes cover every consuming edge of the reference. -/
theorem consumeFresh_inv (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st : St) (ready : Bool)
    (done : List Nat) (ctx : ConsCtx p atoms input idx ci st3) (h : ConsInv p atoms input idx ci st3 st ready done)
    (pc : Nat) (hpc : pc < p.n) (hf : Fresh (st3.slot (ci % p.ring)) pc) :
    let e := (st3.slot (ci % p.ring)).entry pc
    let r := consumeFresh p atoms input st ready pc e.start e.ctr
    ConsInv p atoms input idx ci st3 r.1 r.2 done ∧
    (∀ delta, Consumes p atoms input idx pc delta →
      SlotCov p r.1 (paGen (ci + delta)) ((ci + delta) % p.ring) (p.next pc) (arrPayload p ci st3 pc delta)) ∧
    (∀ d r' w, 1 ≤ d → d < p.ring → r' < p.n →
      SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r' w →
      SlotCov p r.1 (paGen (ci + d)) ((ci + d) % p.ring) r' w) := by
  intro e r
  have main : (p.op pc).consuming = true →
      let rr := consumeProbe p atoms input (consumeArrive p atoms st pc e.start e.ctr) ready pc e.start e.ctr
      ConsInv p atoms input idx ci st3 rr.1 rr.2 done ∧
      (∀ delta, Consumes p atoms input idx pc delta →
        SlotCov p rr.1 (paGen (ci + delta)) ((ci + delta) % p.ring) (p.next pc) (arrPayload p ci st3 pc delta)) ∧
      (∀ d r' w, 1 ≤ d → d < p.ring → r' < p.n →
        SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r' w →
        SlotCov p rr.1 (paGen (ci + d)) ((ci + d) % p.ring) r' w) := by
    intro hop rr
    obtain ⟨inv1, cov1, pers1⟩ := consumeArrive_inv p atoms input idx ci st3 st ready done ctx h pc hpc hf hop
    obtain ⟨inv2, cov2, pers2⟩ := consumeProbe_inv p atoms input idx ci st3 _ ready done ctx inv1 pc hpc hf
    refine ⟨inv2, fun delta hc => ?_, fun d r' w h1 h2 h3 hcv => pers2 d r' w h1 h2 h3 (pers1 d r' w h1 h2 h3 hcv)⟩
    have hd := Consumes_delta p atoms input ctx.awf ctx.ring idx pc delta hc
    by_cases h1 : delta = 1
    · subst h1
      exact pers2 _ _ _ hd.1 hd.2 (ctx.wf.2 pc hpc).1 (cov1 hc)
    · exact cov2 delta (by omega) hc
  have hstale : ¬ (p.op pc).consuming = true →
      ConsInv p atoms input idx ci st3 st ready done ∧
      (∀ delta, Consumes p atoms input idx pc delta →
        SlotCov p st (paGen (ci + delta)) ((ci + delta) % p.ring) (p.next pc) (arrPayload p ci st3 pc delta)) ∧
      (∀ d r' w, 1 ≤ d → d < p.ring → r' < p.n →
        SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r' w →
        SlotCov p st (paGen (ci + d)) ((ci + d) % p.ring) r' w) :=
    fun hop => ⟨h, fun _ hc => absurd hc.1 hop, fun _ _ _ _ _ _ hc => hc⟩
  simp only [r, consumeFresh]
  have hop' : p.op pc = (p.ins.getD pc default).op := rfl
  generalize hop : (p.ins.getD pc default).op = op at hop'
  cases op <;> first
    | exact main (by rw [hop']; rfl)
    | exact hstale (by rw [hop']; simp [Op.consuming])

/-- One live instruction of the current slot. -/
theorem consumeOne_inv (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 st : St) (ready : Bool)
    (done : List Nat) (ctx : ConsCtx p atoms input idx ci st3) (h : ConsInv p atoms input idx ci st3 st ready done)
    (pc : Nat) (hmem : pc ∈ (st3.slot (ci % p.ring)).active) :
    let r := consumeOne p atoms input (ci % p.ring) st ready pc
    ConsInv p atoms input idx ci st3 r.1 r.2 (done ++ [pc]) := by
  intro r
  obtain ⟨hpc, hf⟩ := ctx.rslot.activeLt pc hmem
  simp only [r, consumeOne, h.self]
  have hf' := hf
  unfold Fresh at hf'
  rw [if_neg (by simp [hf'])]
  obtain ⟨inv, cov, _⟩ := consumeFresh_inv p atoms input idx ci st3 st.bumpTests ready done ctx
    (ConsInv_bumpTests p atoms input idx ci st3 st ready done h) pc hpc hf
  refine { inv with arrivals := fun q hq hqn hfq delta hc => ?_ }
  rcases List.mem_append.mp hq with hq | hq
  · exact inv.arrivals q hq hqn hfq delta hc
  · rw [List.mem_singleton] at hq
    subst hq
    exact cov delta hc

/-- The live instructions, in order. -/
theorem consumeList_inv (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 : St)
    (ctx : ConsCtx p atoms input idx ci st3) :
    ∀ (rest : List Nat) (st : St) (ready : Bool) (done : List Nat), ConsInv p atoms input idx ci st3 st ready done →
    (∀ pc ∈ rest, pc ∈ (st3.slot (ci % p.ring)).active) →
    ∃ ready', ConsInv p atoms input idx ci st3 (consumeList p atoms input (ci % p.ring) rest st ready) ready'
      (done ++ rest) := by
  intro rest
  induction rest with
  | nil => intro st ready done h _; exact ⟨ready, by simpa [consumeList] using h⟩
  | cons pc rest ih =>
    intro st ready done h hmem
    simp only [consumeList]
    have inv1 := consumeOne_inv p atoms input idx ci st3 st ready done ctx h pc (hmem pc (List.mem_cons_self ..))
    generalize hr : consumeOne p atoms input (ci % p.ring) st ready pc = r at inv1
    obtain ⟨st1, r1⟩ := r
    dsimp only at inv1 ⊢
    obtain ⟨ready', hinv⟩ := ih st1 r1 (done ++ [pc]) inv1 (fun q hq => hmem q (List.mem_cons_of_mem _ hq))
    rw [List.append_cons]
    exact ⟨ready', hinv⟩

/-- The whole consuming phase from the closed state. -/
theorem paConsume_inv (p : Prog) (atoms : Atoms) (input : Input) (idx ci : Nat) (st3 : St)
    (ctx : ConsCtx p atoms input idx ci st3)
    (hrslot : ∀ d, 1 ≤ d → d < p.ring → RSlotQ p (st3.slot ((ci + d) % p.ring)) (paGen (ci + d)))
    (hsound : ∀ d, 1 ≤ d → d < p.ring → (st3.slot ((ci + d) % p.ring)).gen = paGen (ci + d) →
      ∀ pc, pc < p.n → Fresh (st3.slot ((ci + d) % p.ring)) pc →
        Arrived p atoms input (idx + 1) (thOf (idx + d) (st3.slot ((ci + d) % p.ring)) pc)) :
    ∃ ready', ConsInv p atoms input idx ci st3 (paConsume p atoms input st3 (ci % p.ring)) ready'
      (st3.slot (ci % p.ring)).active := by
  have base : ConsInv p atoms input idx ci st3 st3 false [] :=
    ⟨ctx.ciEq, rfl, rfl, rfl, rfl, rfl, rfl, fun h => absurd h Bool.false_ne_true, hrslot, hsound,
     fun _ _ _ _ _ _ hc => hc, fun _ hq => absurd hq List.not_mem_nil⟩
  obtain ⟨ready', h⟩ := consumeList_inv p atoms input idx ci st3 ctx (st3.slot (ci % p.ring)).active st3 false []
    base (fun _ hq => hq)
  exact ⟨ready', by simpa [paConsume] using h⟩

/-! ## Fields the closure leaves alone, and the end it may record -/

theorem paRelax_fields (k : Nat) (st : St) (si pc start : Nat) (ctr : List Nat) :
    (paRelax k st si pc start ctr).pos = st.pos ∧ (paRelax k st si pc start ctr).cur = st.cur ∧
    (paRelax k st si pc start ctr).eo = st.eo ∧ (paRelax k st si pc start ctr).matched = st.matched ∧
    ((paRelax k st si pc start ctr).slot si).gen = (st.slot si).gen := by
  simp only [paRelax]
  rcases paStore_cases k { st with m := { st.m with relaxes := st.m.relaxes + 1 } } si pc start ctr with h | ⟨h, _⟩
  · rw [h]; exact ⟨rfl, rfl, rfl, rfl, rfl⟩
  · rw [h]
    refine ⟨rfl, rfl, rfl, rfl, ?_⟩
    simp only [ite_true, St.slot_pushQueue, St.slot_charge]
    by_cases hsi : si < st.slots.size
    · rw [St.slot_setSlot_self _ _ _ (by simpa using hsi), storeInto_gen]; rfl
    · unfold St.setSlot
      simp only [St.slot, Array.setIfInBounds, dif_neg hsi]

theorem paConsider_fields (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (e E : Nat)
    (hE : st.matched = true → st.eo ≤ E) (he : e ≤ E) :
    (paConsider k st start ctr e).pos = st.pos ∧ (paConsider k st start ctr e).cur = st.cur ∧
    ((paConsider k st start ctr e).matched = true → (paConsider k st start ctr e).eo ≤ E) := by
  simp only [paConsider]; unfold considerCore
  split
  · exact ⟨rfl, rfl, fun _ => he⟩
  · rename_i h1
    have hm : st.matched = true := by
      cases hmm : st.matched
      · exfalso; apply h1; simp [hmm]
      · rfl
    split
    · exact ⟨rfl, rfl, fun _ => hE hm⟩
    · split
      · exact ⟨rfl, rfl, fun _ => he⟩
      · split
        · exact ⟨rfl, rfl, fun _ => hE hm⟩
        · split
          · exact ⟨rfl, rfl, fun _ => he⟩
          · exact ⟨rfl, rfl, fun _ => hE hm⟩

theorem handleOp_fields (p : Prog) (si : Nat) (st : St) (pc start : Nat) (ctr : List Nat) (E : Nat)
    (hE : st.matched = true → st.eo ≤ E) (hpos : st.pos ≤ E) :
    (handleOp p si st pc start ctr).pos = st.pos ∧ (handleOp p si st pc start ctr).cur = st.cur ∧
    ((handleOp p si st pc start ctr).matched = true → (handleOp p si st pc start ctr).eo ≤ E) ∧
    ((handleOp p si st pc start ctr).slot si).gen = (st.slot si).gen := by
  have hrel : ∀ (s1 : St) q, (s1.matched = true → s1.eo ≤ E) →
      (paRelax p.k s1 si q start ctr).pos = s1.pos ∧ (paRelax p.k s1 si q start ctr).cur = s1.cur ∧
      ((paRelax p.k s1 si q start ctr).matched = true → (paRelax p.k s1 si q start ctr).eo ≤ E) ∧
      ((paRelax p.k s1 si q start ctr).slot si).gen = (s1.slot si).gen := by
    intro s1 q h1
    obtain ⟨a1, a2, a3, a4, a5⟩ := paRelax_fields p.k s1 si q start ctr
    exact ⟨a1, a2, fun hm => by rw [a3]; exact h1 (by rw [← a4]; exact hm), a5⟩
  unfold handleOp
  split
  · obtain ⟨a1, a2, a3, a4⟩ := hrel st _ hE
    obtain ⟨b1, b2, b3, b4⟩ := hrel _ _ a3
    exact ⟨b1.trans a1, b2.trans a2, b3, b4.trans a4⟩
  · exact hrel st _ hE
  · split
    · exact hrel st _ hE
    · exact ⟨rfl, rfl, hE, rfl⟩
  · split
    · exact hrel st _ hE
    · exact ⟨rfl, rfl, hE, rfl⟩
  · obtain ⟨a1, a2, a3⟩ := paConsider_fields p.k st start ctr st.pos E hE hpos
    exact ⟨a1, a2, a3, by rw [paConsider_slot]⟩
  · exact ⟨rfl, rfl, hE, rfl⟩

theorem closureStep_fields (p : Prog) (st : St) (si : Nat) (E : Nat) (hE : st.matched = true → st.eo ≤ E)
    (hpos : st.pos ≤ E) :
    (closureStep p st si).pos = st.pos ∧ (closureStep p st si).cur = st.cur ∧
    ((closureStep p st si).matched = true → (closureStep p st si).eo ≤ E) ∧
    ((closureStep p st si).slot si).gen = (st.slot si).gen := by
  unfold closureStep
  have hc : (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).pos = st.pos ∧
      (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).cur = st.cur ∧
      (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).matched = st.matched ∧
      (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).eo = st.eo ∧
      (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st).slot si = st.slot si := by
    split <;> exact ⟨rfl, rfl, rfl, rfl, rfl⟩
  generalize (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st) = s1 at hc
  obtain ⟨c1, c2, c3, c4, c5⟩ := hc
  rw [← c1, ← c2, ← c5]
  have hE1 : s1.matched = true → s1.eo ≤ E := fun h => by rw [c4]; exact hE (by rw [← c3]; exact h)
  unfold drain
  split
  · exact ⟨rfl, rfl, hE1, rfl⟩
  · unfold handle
    split
    · exact ⟨rfl, rfl, hE1, rfl⟩
    · exact handleOp_fields p si (s1.popQueue _) _ _ _ E hE1 (by rw [← c1] at hpos; exact hpos)

theorem paClosure_fields (p : Prog) (si : Nat) (E : Nat) : ∀ (fuel : Nat) (st : St), (st.matched = true → st.eo ≤ E) →
    st.pos ≤ E →
    (paClosure p si st fuel).pos = st.pos ∧ (paClosure p si st fuel).cur = st.cur ∧
    ((paClosure p si st fuel).matched = true → (paClosure p si st fuel).eo ≤ E) ∧
    ((paClosure p si st fuel).slot si).gen = (st.slot si).gen := by
  intro fuel
  induction fuel with
  | zero => intro st hE _; exact ⟨rfl, rfl, hE, rfl⟩
  | succ fuel ih =>
    intro st hE hpos
    simp only [paClosure]
    split
    · exact ⟨rfl, rfl, hE, rfl⟩
    · obtain ⟨c1, c2, c3, c4⟩ := closureStep_fields p st si E hE hpos
      obtain ⟨i1, i2, i3, i4⟩ := ih (closureStep p st si) c3 (by rw [c1]; exact hpos)
      exact ⟨i1.trans c1, i2.trans c2, i3, i4.trans c4⟩

theorem closeAt_fields (p : Prog) (si : Nat) (st : St) (E : Nat) (hE : st.matched = true → st.eo ≤ E)
    (hpos : st.pos ≤ E) :
    (closeAt p si st).pos = st.pos ∧ (closeAt p si st).cur = st.cur ∧
    ((closeAt p si st).matched = true → (closeAt p si st).eo ≤ E) ∧
    ((closeAt p si st).slot si).gen = (st.slot si).gen :=
  paClosure_fields p si E (closureFuel p st) st hE hpos

theorem spawn_fields (p : Prog) (st : St) (si : Nat) :
    (spawn p st si).pos = st.pos ∧ (spawn p st si).cur = st.cur ∧ (spawn p st si).eo = st.eo ∧
    (spawn p st si).matched = st.matched ∧ ((spawn p st si).slot si).gen = (st.slot si).gen := by
  unfold spawn
  split
  · exact ⟨rfl, rfl, rfl, rfl, rfl⟩
  · exact paRelax_fields p.k st si p.start st.pos (List.replicate p.k 0)

/-! ## Small facts for the boundary loop -/

/-- Domination survives an improvement of the best candidate. -/
theorem Dom_mono (k : Nat) (st st' : St) (hm : st'.matched = true) (hle : selLE (bestOf st') (bestOf st))
    (v : Payload) (h : Dom k st v) : Dom k st' v := by
  rw [Dom_iff] at h ⊢
  obtain ⟨_, h⟩ := h
  simp only [bestOf, selLE] at hle
  refine ⟨hm, ?_⟩
  rcases hle with hlt | ⟨heq, hc | ⟨hc, _⟩⟩
  · left; rcases h with h | ⟨h, _⟩ <;> omega
  · rcases h with h | ⟨h1, hk, h3⟩
    · left; omega
    · right; exact ⟨by omega, hk, ctrLess_trans _ _ _ hc h3⟩
  · rcases h with h | ⟨h1, hk, h3⟩
    · left; omega
    · right; exact ⟨by omega, hk, by rw [hc]; exact h3⟩

theorem SlotCov_ple (p : Prog) (st : St) (g sj r : Nat) (v w : Payload) (h : SlotCov p st g sj r v)
    (hle : ple p.k v w) : SlotCov p st g sj r w := by
  rcases h with ⟨hg, hs, hh⟩ | hd
  · left; exact ⟨hg, hs, ple_trans p.k _ _ _ hh hle⟩
  · right; exact Dom_of_ple p.k st v w hd hle

/-- When the early-stop test finds nothing pending, every current future slot is empty. -/
theorem pendingFrom_false (p : Prog) (st : St) : ∀ (fuel delta : Nat), (pendingFrom p st delta fuel).1 = false →
    ∀ d, delta ≤ d → d < delta + fuel → d < p.ring → (st.slot ((st.ci + d) % p.ring)).gen = paGen (st.ci + d) →
    (st.slot ((st.ci + d) % p.ring)).active = [] := by
  intro fuel
  induction fuel with
  | zero => intro delta _ d h1 h2; omega
  | succ fuel ih =>
    intro delta h d h1 h2 h3 hg
    simp only [pendingFrom] at h
    split at h
    · split at h
      · simp at h
      · rename_i hne
        generalize hpf : pendingFrom p st (delta + 1) fuel = rc at h
        obtain ⟨r, c⟩ := rc
        simp only at h
        by_cases hd : d = delta
        · subst hd
          by_cases ha : (st.slot ((st.ci + d) % p.ring)).active = []
          · exact ha
          · exfalso; apply hne; simp [hg, ha]
        · exact ih (delta + 1) (by rw [hpf]; exact h) d (by omega) (by omega) h3 hg
    · omega

theorem chainPos_ge (input : Input) : ∀ i, chainPos input i < input.bytes.size → i ≤ chainPos input i := by
  intro i
  induction i with
  | zero => intro _; exact Nat.zero_le _
  | succ i ih =>
    intro h
    rw [chainPos_succ] at h ⊢
    have h1 : chainPos input i < input.bytes.size := Nat.lt_of_le_of_lt (stepPos_ge input _) h
    have := ih h1
    have := stepPos_gt input _ h1
    omega

theorem valid_le (input : Input) (idx : Nat) (hv : ValidIdx input idx) : idx ≤ input.bytes.size := by
  rcases hv with rfl | h
  · exact Nat.zero_le _
  · have := chainPos_ge input (idx - 1) h; omega

/-- The facts about the state after the current slot was opened. -/
structure Opened (p : Prog) (atoms : Atoms) (input : Input) (st st1 : St) (idx : Nat) : Prop where
  gen : (st1.slot (st.ci % p.ring)).gen = paGen st.ci
  rslot : RSlot p (st1.slot (st.ci % p.ring))
  active : (st1.slot (st.ci % p.ring)).active = liveAt st (st.ci % p.ring) (paGen st.ci)
  sound : ∀ pc, pc < p.n → Fresh (st1.slot (st.ci % p.ring)) pc →
    Reach p atoms input (thOf idx (st1.slot (st.ci % p.ring)) pc)
  arr : ∀ T, Arrived p atoms input idx T → T.i = idx → Prod p atoms input T →
    Holds p.k (st1.slot (st.ci % p.ring)) T.pc T.payload ∨ Dom p.k st T.payload
  other : ∀ sj, sj ≠ st.ci % p.ring → st1.slot sj = st.slot sj
  pos : st1.pos = st.pos
  ci : st1.ci = st.ci
  best : bestOf st1 = bestOf st
  matched : st1.matched = st.matched
  size : st1.slots.size = st.slots.size

theorem opened_of_open (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int) (idx : Nat)
    (hinv : RunInv p atoms input st prev idx) (hring : 2 ≤ p.ring) :
    Opened p atoms input st (st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)) idx := by
  obtain ⟨h1, h2, h3, _, h5, h6, h7, h8, h9, h10, h11, _, h13, _⟩ := open_boundary p atoms input st prev idx hinv hring
  exact ⟨h1, h2, h3, h5, h6, h7, h8, h9, h10, h11, h13⟩

theorem Opened_bumpSkipped (p : Prog) (atoms : Atoms) (input : Input) (st st1 : St) (idx : Nat)
    (h : Opened p atoms input st st1 idx) (n : Nat) : Opened p atoms input st (st1.bumpSkipped n) idx :=
  ⟨h.gen, h.rslot, h.active, h.sound, h.arr, h.other, h.pos, h.ci, h.best, h.matched, h.size⟩

/-- What holds of the state when the loop stops: the best is a candidate that the order puts first. -/
def FinalOK (p : Prog) (atoms : Atoms) (input : Input) (st : St) : Prop :=
  (st.matched = true → Cand p atoms input st.so st.bestCtr st.eo) ∧
  (∀ s c e, Cand p atoms input s c e → st.matched = true ∧ selLE (bestOf st) (s, c, e))

/--
What the run assumes of the scan filter, stated against the reference. When the filter is enabled the ring
has two slots, and a jump from a boundary where `^` does not hold lands on a boundary of the subject such that
no thread spawned at a skipped boundary can accept, and the previous character after the jump is a newline
exactly when the reference says so.
-/
def ScanSound (p : Prog) (atoms : Atoms) (input : Input) : Prop :=
  p.scan.enabled = true →
    p.ring = 2 ∧
    ∀ i, ValidIdx input i → chainPos input i < input.bytes.size → ¬ bolRef input i →
      chainPos input i < (scanAhead p input (chainPos input i)).1 →
      ∃ j, ValidIdx input j ∧ chainPos input j = (scanAhead p input (chainPos input i)).1 ∧
        (∀ i', i ≤ i' → i' < j → ¬ Prod p atoms input (spawnTh p input i')) ∧
        (prevAfterJump input (scanAhead p input (chainPos input i)).1 = 10 ↔
          curAt input (chainPos input (j - 1)) = 10)

/-! ## The boundary body: closure, consumption, and the stop test -/

/-- After the consuming phase, every productive arrival from this boundary or before is covered. -/
theorem consume_complete (p : Prog) (atoms : Atoms) (input : Input) (st st3 st4 : St) (prev : Int) (idx : Nat)
    (ready : Bool) (hwf : p.wf) (hring : 2 ≤ p.ring)
    (hinv : RunInv p atoms input st prev idx)
    (hother3 : ∀ sj, sj ≠ st.ci % p.ring → st3.slot sj = st.slot sj)
    (hdom3 : ∀ v, Dom p.k st v → Dom p.k st3 v)
    (hrs3 : RSlot p (st3.slot (st.ci % p.ring)))
    (hcov3 : ∀ T, Reach p atoms input T → T.i = idx → Prod p atoms input T → Covered p.k st3 (st.ci % p.ring) T)
    (h4 : ConsInv p atoms input idx st.ci st3 st4 ready (st3.slot (st.ci % p.ring)).active) :
    ∀ d, 1 ≤ d → d < p.ring → ∀ T, Arrived p atoms input (idx + 1) T → T.i = idx + d → Prod p atoms input T →
      SlotCov p st4 (paGen (st.ci + d)) ((st.ci + d) % p.ring) T.pc T.payload := by
  intro d hd1 hd2 T ha hi hp
  obtain ⟨T0, delta, hr0, hlt, hc0, hT⟩ := ha
  subst hT
  have hpc0 : T0.pc < p.n := Reach_pc p atoms input hwf T0 hr0
  have hnext : p.next T0.pc < p.n := (hwf.2 T0.pc hpc0).1
  have hi' : T0.i + delta = idx + d := hi
  by_cases h0 : T0.i < idx
  · have ha' : Arrived p atoms input idx ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩ :=
      ⟨T0, delta, hr0, h0, hc0, rfl⟩
    have hcov : SlotCov p st (paGen (st.ci + d)) ((st.ci + d) % p.ring) (p.next T0.pc) (T0.s, bumpIf p T0.pc T0.c delta) :=
      hinv.complete d (by omega) _ ha' hi hp
    have hcov3 : SlotCov p st3 (paGen (st.ci + d)) ((st.ci + d) % p.ring) (p.next T0.pc)
        (T0.s, bumpIf p T0.pc T0.c delta) := by
      rcases hcov with ⟨hg, hh⟩ | hdm
      · left; rw [hother3 _ (mod_add_ne st.ci d p.ring hring hd1 hd2)]; exact ⟨hg, hh⟩
      · right; exact hdom3 _ hdm
    exact h4.persist d _ _ hd1 hd2 hnext hcov3
  · have h0' : T0.i = idx := by omega
    have hp0 : Prod p atoms input T0 := Prod_of_step p atoms input T0 _ (Step.consume T0 delta hc0) hp
    rcases hcov3 T0 hr0 h0' hp0 with ⟨hs, hle⟩ | hdm
    · have hf : Fresh (st3.slot (st.ci % p.ring)) T0.pc := hs
      have hmem : T0.pc ∈ (st3.slot (st.ci % p.ring)).active := hrs3.freshActive T0.pc hpc0 hf
      have hcons' : Consumes p atoms input idx T0.pc delta := by rw [← h0']; exact hc0
      have := h4.arrivals T0.pc hmem hpc0 hf delta hcons'
      have hdd : d = delta := by omega
      subst hdd
      exact SlotCov_ple p st4 _ _ _ _ _ this (ple_bumpIf p T0.pc d _ _ hle)
    · right
      exact Dom_of_bestOf p.k st3 st4 h4.best h4.matched _
        (Dom_step p atoms input st3 T0 _ (Step.consume T0 delta hc0) hdm)

/-- Advancing to the next boundary keeps the run invariant. -/
theorem advance_inv (p : Prog) (atoms : Atoms) (input : Input) (st st3 st4 st5 : St) (idx : Nat)
    (ready : Bool) (hawf : atoms.wf2 p) (hring : 2 ≤ p.ring)
    (hposLt : chainPos input idx < input.bytes.size)
    (hinv3 : CInvX p atoms input idx (st.ci % p.ring) st3 none)
    (hgen3 : (st3.slot (st.ci % p.ring)).gen = paGen st.ci)
    (hcur3 : st3.cur = curAt input (chainPos input idx))
    (heo3 : st3.matched = true → st3.eo ≤ chainPos input idx)
    (hseen3 : ∀ s c e, Cand p atoms input s c e → e ≤ chainPos input idx →
      st3.matched = true ∧ selLE (bestOf st3) (s, c, e))
    (h4 : ConsInv p atoms input idx st.ci st3 st4 ready (st3.slot (st.ci % p.ring)).active)
    (hcomp4 : ∀ d, 1 ≤ d → d < p.ring → ∀ T, Arrived p atoms input (idx + 1) T → T.i = idx + d →
      Prod p atoms input T → SlotCov p st4 (paGen (st.ci + d)) ((st.ci + d) % p.ring) T.pc T.payload)
    (hslots5 : st5.slots = st4.slots) (hci5 : st5.ci = st.ci + 1) (hpos5 : st5.pos = chainPos input (idx + 1))
    (hbest5 : bestOf st5 = bestOf st4) (hm5 : st5.matched = st4.matched) :
    RunInv p atoms input st5 st4.cur (idx + 1) := by
  have hs5 : ∀ si, st5.slot si = st4.slot si := slot_of_slots st4 st5 hslots5
  have hb := hbest5.trans h4.best
  simp only [bestOf, Prod.mk.injEq] at hb
  obtain ⟨hso, hbc, heo⟩ := hb
  have hmm : st5.matched = st3.matched := hm5.trans h4.matched
  have hlast : ∀ d, d < p.ring → ¬ d + 1 < p.ring → (st.ci + 1 + d) % p.ring = st.ci % p.ring := by
    intro d _ h; rw [show st.ci + 1 + d = st.ci + p.ring by omega, Nat.add_mod_right]
  have hposStep : chainPos input idx < chainPos input (idx + 1) := by
    rw [chainPos_succ]; exact stepPos_gt input _ hposLt
  refine ⟨by rw [hslots5, h4.size, hinv3.slotsSize], hpos5, Or.inr (by rw [Nat.add_sub_cancel]; exact hposLt),
    ?_, ?_, ?_, ?_, ?_, ?_, ?_⟩
  · rw [h4.cur, hcur3, Nat.add_sub_cancel]
    exact ⟨fun h => ⟨by omega, h⟩, fun h => h.2⟩
  · intro d hd
    rw [hs5, hci5]
    by_cases hlt : d + 1 < p.ring
    · rw [show st.ci + 1 + d = st.ci + (d + 1) by omega]; exact h4.rslot (d + 1) (by omega) hlt
    · rw [hlast d hd hlt, h4.self]
      exact RSlotQ.ofRSlot p _ _ hinv3.rslot (by rw [hgen3]; unfold paGen; omega)
  · intro d hd hg pc hpc hf
    rw [hs5, hci5] at hg hf ⊢
    by_cases hlt : d + 1 < p.ring
    · rw [show st.ci + 1 + d = st.ci + (d + 1) by omega] at hg hf ⊢
      rw [show idx + 1 + d = idx + (d + 1) by omega]
      exact h4.sound (d + 1) (by omega) hlt hg pc hpc hf
    · exfalso
      rw [hlast d hd hlt, h4.self, hgen3] at hg
      unfold paGen at hg; omega
  · intro d hd T ha hi hp
    have hdlt : d + 1 < p.ring := by
      obtain ⟨T0, delta, _, hlt, hc0, hT⟩ := ha
      have := Consumes_delta p atoms input hawf hring T0.i T0.pc delta hc0
      have : T.i = T0.i + delta := by rw [hT]
      omega
    rw [hs5, hci5, show st.ci + 1 + d = st.ci + (d + 1) by omega]
    have := hcomp4 (d + 1) (by omega) hdlt T ha (by omega) hp
    rcases this with ⟨hg, hh⟩ | hdm
    · exact Or.inl ⟨hg, hh⟩
    · exact Or.inr (Dom_of_bestOf p.k st4 st5 hbest5 hm5 _ hdm)
  · rw [hbc]; exact hinv3.bestLen
  · intro hm
    have hm3 : st3.matched = true := hmm ▸ hm
    refine ⟨by rw [hso, hbc, heo]; exact hinv3.bestCand hm3, ?_⟩
    rw [heo, hpos5]
    exact Nat.lt_of_le_of_lt (heo3 hm3) hposStep
  · intro s c e hc he
    rw [hpos5] at he
    have hele : e ≤ chainPos input idx := by
      obtain ⟨i, pc, hr, _, rfl⟩ := hc
      by_cases hle : i ≤ idx
      · exact chainPos_mono input i idx hle
      · exfalso
        have := chainPos_mono input (idx + 1) i (by omega)
        omega
    obtain ⟨hm3, hle3⟩ := hseen3 s c e hc hele
    refine ⟨by rw [hmm]; exact hm3, ?_⟩
    rw [hbest5, h4.best]; exact hle3

/-- The early stop: a match is known and no future slot is live, so nothing can beat it. -/
theorem stop_final (p : Prog) (atoms : Atoms) (input : Input) (st st3 st4 st5 : St) (idx : Nat)
    (ready : Bool) (hwf : p.wf) (hawf : atoms.wf2 p) (hring : 2 ≤ p.ring)
    (hposLt : chainPos input idx < input.bytes.size)
    (hinv3 : CInvX p atoms input idx (st.ci % p.ring) st3 none)
    (heo3 : st3.matched = true → st3.eo ≤ chainPos input idx)
    (hseen3 : ∀ s c e, Cand p atoms input s c e → e ≤ chainPos input idx →
      st3.matched = true ∧ selLE (bestOf st3) (s, c, e))
    (h4 : ConsInv p atoms input idx st.ci st3 st4 ready (st3.slot (st.ci % p.ring)).active)
    (hcomp4 : ∀ d, 1 ≤ d → d < p.ring → ∀ T, Arrived p atoms input (idx + 1) T → T.i = idx + d →
      Prod p atoms input T → SlotCov p st4 (paGen (st.ci + d)) ((st.ci + d) % p.ring) T.pc T.payload)
    (hm4 : st4.matched = true) (hnp : (pendingFrom p st4 1 p.ring).1 = false)
    (hbest5 : bestOf st5 = bestOf st4) (hm5 : st5.matched = st4.matched) :
    FinalOK p atoms input st5 := by
  have hb := hbest5.trans h4.best
  simp only [bestOf, Prod.mk.injEq] at hb
  obtain ⟨hso, hbc, heo⟩ := hb
  have hm3 : st3.matched = true := h4.matched ▸ hm4
  have hcand3 := hinv3.bestCand hm3
  have hso3 : st3.so ≤ st3.eo := by
    obtain ⟨i, pc, hr, _, he⟩ := hcand3
    have := Reach_start_le p atoms input _ hr
    simp only at this; omega
  have hposStep : chainPos input idx < chainPos input (idx + 1) := by
    rw [chainPos_succ]; exact stepPos_gt input _ hposLt
  refine ⟨fun _ => by rw [hso, hbc, heo]; exact hcand3, fun s c e hc => ?_⟩
  refine ⟨by rw [hm5]; exact hm4, ?_⟩
  rw [hbest5, h4.best]
  by_cases hle : e ≤ chainPos input idx
  · exact (hseen3 s c e hc hle).2
  · obtain ⟨i, pc, hr, hacc, rfl⟩ := hc
    obtain ⟨i0, hv0, hs⟩ := hr
    have hs0 := Steps_start p atoms input _ _ hs
    simp only [spawnTh] at hs0
    have hidx : idx < i := Nat.lt_of_not_le (fun h => hle (chainPos_mono input i idx h))
    by_cases hi0 : i0 ≤ idx
    · obtain ⟨T0, delta, hs1, hle1, hc1, hlt1, hs2⟩ :=
        Steps_cross p atoms input idx (spawnTh p input i0) ⟨i, pc, s, c⟩ hs hi0 hidx
      have hd := Consumes_delta p atoms input hawf hring T0.i T0.pc delta hc1
      have hr0 : Reach p atoms input T0 := ⟨i0, hv0, hs1⟩
      have ha : Arrived p atoms input (idx + 1) ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩ :=
        ⟨T0, delta, hr0, by omega, hc1, rfl⟩
      have hp1 : Prod p atoms input ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩ :=
        ⟨⟨i, pc, s, c⟩, hs2, hacc⟩
      have hpc1 : p.next T0.pc < p.n := Reach_pc p atoms input hwf _ (Arrived_reach p atoms input _ _ ha)
      have hcv := hcomp4 (T0.i + delta - idx) (by omega) (by omega) _ ha
        (by show T0.i + delta = idx + (T0.i + delta - idx); omega) hp1
      rcases hcv with ⟨hg, hs4, _⟩ | hdm
      · exfalso
        have hf : Fresh (st4.slot ((st.ci + (T0.i + delta - idx)) % p.ring)) (p.next T0.pc) := hs4
        have hmem := ((h4.rslot _ (by omega) (by omega)).cur hg).freshActive _ hpc1 hf
        have hempty := pendingFrom_false p st4 p.ring 1 hnp (T0.i + delta - idx) (by omega) (by omega) (by omega)
          (by rw [h4.ciEq]; exact hg)
        rw [h4.ciEq] at hempty
        rw [hempty] at hmem
        exact List.not_mem_nil hmem
      · have := Dom_steps p atoms input st4 _ _ hs2 hdm
        rw [← h4.best]
        exact selLE_of_Dom p.k st4 s c _ this
    · simp only [bestOf, selLE]
      left
      have := chainPos_mono input (idx + 1) i0 (by omega)
      have hm3' := heo3 hm3
      omega

/-- One boundary's body: it either ends the run with the right answer, or advances with the invariant. -/
theorem body_correct (p : Prog) (atoms : Atoms) (input : Input) (st st1 : St) (prev : Int) (idx : Nat)
    (hwf : p.wf) (hawf : atoms.wf2 p) (hring : 2 ≤ p.ring)
    (hinv : RunInv p atoms input st prev idx) (hop : Opened p atoms input st st1 idx) :
    let r := boundaryBody p atoms input st1 (st.ci % p.ring) prev (liveAt st (st.ci % p.ring) (paGen st.ci))
    (r.2 = none → FinalOK p atoms input r.1) ∧
    (∀ prev', r.2 = some prev' → RunInv p atoms input r.1 prev' (idx + 1)) := by
  intro r
  have hsi : st.ci % p.ring < p.ring := Nat.mod_lt _ (by omega)
  have hsize1 : st1.slots.size = p.ring := hop.size.trans hinv.slotsSize
  have hpos1 : st1.pos = chainPos input idx := hop.pos.trans hinv.posEq
  have hb1 := hop.best
  simp only [bestOf, Prod.mk.injEq] at hb1
  obtain ⟨hso1, hbc1, heo1⟩ := hb1
  -- The state the closure starts from.
  obtain ⟨hslotC, _, _, _, _, _, _, _, _, _, hcurC⟩ :=
    flags_copy st1 input prev (liveAt st (st.ci % p.ring) (paGen st.ci))
  obtain ⟨hinv2, hspawn2, harr2, hbest2, hm2, hci2, hpos2, hother2, hsize2⟩ :=
    body_start p atoms input hwf hring st1 prev idx (st.ci % p.ring) (liveAt st (st.ci % p.ring) (paGen st.ci))
      hsize1 hsi hop.rslot hop.active hpos1 hinv.valid hinv.prevOK hop.sound
      (fun T ha hi hp => (hop.arr T ha hi hp).imp_right (Dom_of_bestOf p.k st st1 hop.best hop.matched _))
      (by rw [hbc1]; exact hinv.bestLen)
      (fun hm => by
        have := hinv.bestCand (hop.matched ▸ hm)
        rw [hso1, hbc1, heo1, hop.pos]; exact this)
  obtain ⟨_, hcurS, _, _, hgenS⟩ :=
    spawn_fields p ((st1.setFlags input prev).copyQueue (liveAt st (st.ci % p.ring) (paGen st.ci))) (st.ci % p.ring)
  generalize hst2 : spawn p ((st1.setFlags input prev).copyQueue (liveAt st (st.ci % p.ring) (paGen st.ci)))
    (st.ci % p.ring) = st2 at hinv2 hspawn2 harr2 hbest2 hm2 hci2 hpos2 hother2 hsize2 hcurS hgenS
  have hpos2' : st2.pos = chainPos input idx := hpos2.trans hpos1
  have hcur2 : st2.cur = curAt input (chainPos input idx) := by rw [hcurS, hcurC, hpos1]
  have hci2' : st2.ci = st.ci := hci2.trans hop.ci
  have hm2' : st2.matched = st.matched := hm2.trans hop.matched
  have hbest2' : bestOf st2 = bestOf st := hbest2.trans hop.best
  have heo2 : st2.matched = true → st2.eo ≤ chainPos input idx := by
    intro hm
    have hb := hbest2'
    simp only [bestOf, Prod.mk.injEq] at hb
    rw [hb.2.2]
    have := (hinv.bestCand (hm2' ▸ hm)).2
    rw [hinv.posEq] at this; omega
  have hgen2 : (st2.slot (st.ci % p.ring)).gen = paGen st.ci := by rw [hgenS, hslotC]; exact hop.gen
  obtain ⟨hinv3, _, hcov3, hmono3, hcand3, hpos3, hci3, hother3, _⟩ :=
    closure_result p atoms input hwf hawf idx (st.ci % p.ring) st2 hinv.valid hinv2 hspawn2 harr2
  obtain ⟨_, hcurF, heoF, hgenF⟩ :=
    closeAt_fields p (st.ci % p.ring) st2 (chainPos input idx) heo2 (Nat.le_of_eq hpos2')
  generalize hst3 : closeAt p (st.ci % p.ring) st2 = st3 at hinv3 hcov3 hmono3 hcand3 hpos3 hci3 hother3 hcurF heoF hgenF
  have hpos3' : st3.pos = chainPos input idx := hpos3.trans hpos2'
  have hcur3 : st3.cur = curAt input (chainPos input idx) := hcurF.trans hcur2
  have hci3' : st3.ci = st.ci := hci3.trans hci2'
  have hgen3 : (st3.slot (st.ci % p.ring)).gen = paGen st.ci := hgenF.trans hgen2
  have hother3' : ∀ sj, sj ≠ st.ci % p.ring → st3.slot sj = st.slot sj :=
    fun sj h => (hother3 sj h).trans ((hother2 sj h).trans (hop.other sj h))
  have hdom3 : ∀ v, Dom p.k st v → Dom p.k st3 v := by
    intro v hv
    have hmst : st.matched = true := ((Dom_iff p.k st v).mp hv).1
    obtain ⟨hm3, hle3⟩ := hmono3 (by rw [hm2']; exact hmst)
    exact Dom_mono p.k st st3 hm3 (by rw [hbest2'] at hle3; exact hle3) v hv
  have hseen3 : ∀ s c e, Cand p atoms input s c e → e ≤ chainPos input idx →
      st3.matched = true ∧ selLE (bestOf st3) (s, c, e) := by
    intro s c e hc he
    rcases Nat.lt_or_eq_of_le he with hlt | heq
    · obtain ⟨hm, hle⟩ := hinv.seen s c e hc (by rw [hinv.posEq]; exact hlt)
      obtain ⟨hm3, hle3⟩ := hmono3 (by rw [hm2']; exact hm)
      exact ⟨hm3, selLE_trans _ _ _ hle3 (by rw [hbest2']; exact hle)⟩
    · exact hcand3 s c e hc heq
  -- The end of the subject, or the consuming phase.
  simp only [r, boundaryBody]
  rw [hst2, hst3]
  by_cases hend : st1.pos = input.bytes.size
  · rw [if_pos (by simpa using hend)]
    refine ⟨fun _ => ⟨hinv3.bestCand, fun s c e hc => ?_⟩, fun _ h => by simp at h⟩
    have heL : e ≤ input.bytes.size := by
      obtain ⟨i, _, _, _, rfl⟩ := hc
      exact chainPos_le_size input i
    exact hseen3 s c e hc (by rw [← hpos1, hend]; exact heL)
  · rw [if_neg (by simpa using hend)]
    have hposLt : chainPos input idx < input.bytes.size :=
      Nat.lt_of_le_of_ne (chainPos_le_size input idx) (by rw [← hpos1]; exact hend)
    have ctx : ConsCtx p atoms input idx st.ci st3 :=
      ⟨hci3', hwf, hawf, hring, hinv3.slotsSize, hpos3', hposLt, hcur3, hinv3.rslot, hinv3.sound⟩
    obtain ⟨ready, h4⟩ := paConsume_inv p atoms input idx st.ci st3 ctx
      (fun d hd1 hd2 => by rw [hother3' _ (mod_add_ne st.ci d p.ring hring hd1 hd2)]; exact hinv.slots d hd2)
      (fun d hd1 hd2 hg pc hpc hf => by
        rw [hother3' _ (mod_add_ne st.ci d p.ring hring hd1 hd2)] at hg hf ⊢
        exact Arrived_mono p atoms input idx (idx + 1) _ (Nat.le_succ _) (hinv.sound d hd2 hg pc hpc hf))
    have hcomp4 := consume_complete p atoms input st st3 _ prev idx ready hwf hring hinv hother3' hdom3
      hinv3.rslot hcov3 h4
    generalize hst4 : paConsume p atoms input st3 (st.ci % p.ring) = st4 at h4 hcomp4
    have hsizeStep : chainPos input (idx + 1) = st1.pos + (decodeRuneAt input.bytes st1.pos).2 := by
      rw [chainPos_succ, ← hpos1]
      unfold stepPos sizeAt
      rw [if_neg (by simpa using hend)]
    have hpos5 : st4.pos + (decodeRuneAt input.bytes st1.pos).2 = chainPos input (idx + 1) := by
      rw [h4.pos, hpos3', ← hpos1, hsizeStep]
    have hci5 : st4.ci + 1 = st.ci + 1 := by rw [h4.ciEq]
    simp only [afterConsume]
    by_cases hm4 : st4.matched = true
    · rw [if_pos hm4]
      by_cases hnp : (pendingFrom p st4 1 p.ring).1 = true
      · rw [if_pos hnp]
        refine ⟨fun h => by simp at h, fun prev' h => ?_⟩
        simp only [Option.some.injEq] at h
        subst h
        exact advance_inv p atoms input st st3 st4 _ idx ready hawf hring hposLt hinv3 hgen3 hcur3
          heoF hseen3 h4 hcomp4 rfl hci5 hpos5 rfl rfl
      · rw [if_neg hnp]
        simp only [Bool.not_eq_true] at hnp
        refine ⟨fun _ => ?_, fun _ h => by simp at h⟩
        exact stop_final p atoms input st st3 st4 _ idx ready hwf hawf hring hposLt hinv3 heoF hseen3 h4 hcomp4
          hm4 hnp rfl rfl
    · rw [if_neg hm4]
      refine ⟨fun h => by simp at h, fun prev' h => ?_⟩
      simp only [Option.some.injEq] at h
      subst h
      exact advance_inv p atoms input st st3 st4 _ idx ready hawf hring hposLt hinv3 hgen3 hcur3
        heoF hseen3 h4 hcomp4 rfl hci5 hpos5 rfl rfl

/-! ## The scan filter's jump -/

/-- A jump of the scan filter keeps the run invariant at the boundary it lands on. -/
theorem skip_inv (p : Prog) (atoms : Atoms) (input : Input) (st st1 : St) (prev : Int) (idx : Nat)
    (hwf : p.wf) (hawf : atoms.wf2 p) (hring : 2 ≤ p.ring) (hscan : ScanSound p atoms input)
    (hinv : RunInv p atoms input st prev idx) (hop : Opened p atoms input st st1 idx)
    (hempty : liveAt st (st.ci % p.ring) (paGen st.ci) = []) (hm : st.matched = false)
    (hen : p.scan.enabled = true) (hposLt : st.pos < input.bytes.size) (hbol : bolAt input st.pos prev = false)
    (hjump : st.pos < (scanAhead p input st.pos).1) (n : Nat) :
    ∃ j, idx < j ∧ RunInv p atoms input ((st1.bumpSkipped n).jumpTo (scanAhead p input st.pos).1)
      (prevAfterJump input (scanAhead p input st.pos).1) j := by
  obtain ⟨hring2, hs⟩ := hscan hen
  rw [hinv.posEq] at hposLt hbol hjump ⊢
  have hnb : ¬ bolRef input idx := fun h => by
    have := (bol_eq_of_prevOK input idx prev hinv.valid hinv.prevOK).mpr h
    rw [hbol] at this; exact Bool.false_ne_true this
  obtain ⟨j, hvj, hcj, hunprod, hprev⟩ := hs idx hinv.valid hposLt hnb hjump
  have hij : idx < j := Nat.lt_of_not_le (fun hle => by have := chainPos_mono input j idx hle; omega)
  -- No productive thread of the reference sits at a boundary the filter skips.
  have hgap : ∀ T, Reach p atoms input T → idx ≤ T.i → T.i < j → ¬ Prod p atoms input T := by
    intro T hr hge hlt hp
    obtain ⟨i0, hv0, hs0⟩ := hr
    by_cases h0 : idx ≤ i0
    · have hi0 := Steps_i p atoms input _ _ hs0
      simp only [spawnTh] at hi0
      exact hunprod i0 h0 (by omega) (Prod_of_steps p atoms input _ _ hs0 hp)
    · obtain ⟨T0, delta, hs1, _, hc1, hlt1, hs2⟩ :=
        Steps_cross p atoms input (idx - 1) (spawnTh p input i0) T hs0 (by show i0 ≤ idx - 1; omega) (by omega)
      have hd := Consumes_delta p atoms input hawf hring T0.i T0.pc delta hc1
      have hr0 : Reach p atoms input T0 := ⟨i0, hv0, hs1⟩
      have ha : Arrived p atoms input idx ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩ :=
        ⟨T0, delta, hr0, by omega, hc1, rfl⟩
      have hp1 : Prod p atoms input ⟨T0.i + delta, p.next T0.pc, T0.s, bumpIf p T0.pc T0.c delta⟩ :=
        Prod_of_steps p atoms input _ _ hs2 hp
      have hpc1 : p.next T0.pc < p.n := Reach_pc p atoms input hwf _ (Arrived_reach p atoms input _ _ ha)
      rcases hinv.complete 0 (by omega) _ ha (by show T0.i + delta = idx + 0; omega) hp1 with ⟨hg, hs4, _⟩ | hdm
      · have hf : Fresh (st.slot ((st.ci + 0) % p.ring)) (p.next T0.pc) := hs4
        have hmem := ((hinv.slots 0 (by omega)).cur hg).freshActive _ hpc1 hf
        have hlive : p.next T0.pc ∈ liveAt st (st.ci % p.ring) (paGen st.ci) := by
          rw [liveAt_iff]
          simp only [Nat.add_zero] at hmem hf hg
          exact ⟨hmem, by unfold Fresh at hf; rw [hf, hg]⟩
        rw [hempty] at hlive
        exact List.not_mem_nil hlive
      · rw [Dom_iff] at hdm; rw [hm] at hdm; exact Bool.false_ne_true hdm.1
  have hb1 := hop.best
  simp only [bestOf, Prod.mk.injEq] at hb1
  obtain ⟨_, hbc1, _⟩ := hb1
  refine ⟨j, hij, ?_⟩
  refine ⟨?_, ?_, hvj, ?_, ?_, ?_, ?_, ?_, ?_, ?_⟩
  · show st1.slots.size = p.ring; exact hop.size.trans hinv.slotsSize
  · show (scanAhead p input (chainPos input idx)).1 = chainPos input j; exact hcj.symm
  · exact ⟨fun h => ⟨by omega, hprev.mp h⟩, fun h => hprev.mpr h.2⟩
  · intro d hd
    show RSlotQ p (st1.slot ((st1.ci + 1 + d) % p.ring)) (paGen (st1.ci + 1 + d))
    rw [hop.ci]
    rcases (by omega : d = 0 ∨ d = 1) with rfl | rfl
    · rw [Nat.add_zero, hop.other _ (mod_add_ne st.ci 1 p.ring hring (Nat.le_refl _) (by omega))]
      exact hinv.slots 1 (by omega)
    · rw [show st.ci + 1 + 1 = st.ci + p.ring by omega, Nat.add_mod_right]
      exact RSlotQ.ofRSlot p _ _ hop.rslot (by rw [hop.gen]; unfold paGen; omega)
  · intro d hd hg pc hpc hf
    exfalso
    change (st1.slot ((st1.ci + 1 + d) % p.ring)).gen = paGen (st1.ci + 1 + d) at hg
    change Fresh (st1.slot ((st1.ci + 1 + d) % p.ring)) pc at hf
    rw [hop.ci] at hg hf
    rcases (by omega : d = 0 ∨ d = 1) with rfl | rfl
    · rw [Nat.add_zero, hop.other _ (mod_add_ne st.ci 1 p.ring hring (Nat.le_refl _) (by omega))] at hg hf
      obtain ⟨T0, delta, _, hlt, hc0, hT⟩ := hinv.sound 1 (by omega) hg pc hpc hf
      have hd := Consumes_delta p atoms input hawf hring T0.i T0.pc delta hc0
      have : idx + 1 = T0.i + delta := by
        have := congrArg Th.i hT; simpa [thOf] using this
      omega
    · rw [show st.ci + 1 + 1 = st.ci + p.ring by omega, Nat.add_mod_right, hop.gen] at hg
      unfold paGen at hg; omega
  · intro d _ T ha hi hp
    exfalso
    obtain ⟨T0, delta, hr0, hlt, hc0, hT⟩ := ha
    have hd' := Consumes_delta p atoms input hawf hring T0.i T0.pc delta hc0
    have hTi : T.i = T0.i + delta := by rw [hT]
    have hp0 : Prod p atoms input T0 :=
      Prod_of_step p atoms input T0 T (by rw [hT]; exact Step.consume T0 delta hc0) hp
    exact hgap T0 hr0 (by omega) hlt hp0
  · show st1.bestCtr.length = p.k; rw [hbc1]; exact hinv.bestLen
  · intro hmm
    exfalso
    have : st1.matched = true := hmm
    rw [hop.matched, hm] at this
    exact Bool.false_ne_true this
  · intro s c e hc he
    exfalso
    change e < (scanAhead p input (chainPos input idx)).1 at he
    obtain ⟨i, pc, hr, hacc, rfl⟩ := hc
    have hvi := Reach_valid p atoms input hawf _ hr
    by_cases hlt : chainPos input i < chainPos input idx
    · have := (hinv.seen _ _ _ ⟨i, pc, hr, hacc, rfl⟩ (by rw [hinv.posEq]; exact hlt)).1
      rw [hm] at this; exact Bool.false_ne_true this
    · have hge : idx ≤ i := by
        by_cases h : idx ≤ i
        · exact h
        · exfalso
          have h1 := chainPos_mono input i idx (by omega)
          have := chainPos_inj input i idx hvi hinv.valid (by omega)
          omega
      have hij' : i < j := Nat.lt_of_not_le (fun h => by have := chainPos_mono input j i h; omega)
      exact hgap ⟨i, pc, s, c⟩ hr hge hij' ⟨_, Steps.refl _, hacc⟩

/-! ## The decidable well-formedness check -/

/-- `Prog.wfCheck` decides the program hypotheses of the universal theorems. -/
theorem wfCheck_sound (p : Prog) (h : p.wfCheck = true) : p.wf ∧ (p.scan.enabled = true → p.ring = 2) := by
  unfold Prog.wfCheck at h
  simp only [Bool.and_eq_true, decide_eq_true_eq, List.all_eq_true, List.mem_range, Bool.or_eq_true,
    Bool.not_eq_true', beq_iff_eq] at h
  obtain ⟨⟨h1, h2⟩, h3⟩ := h
  refine ⟨⟨h1, fun pc hpc => h2 pc hpc⟩, fun he => ?_⟩
  rcases h3 with h3 | h3
  · rw [he] at h3; exact absurd h3.symm Bool.false_ne_true
  · exact h3

/-! ## The boundary loop -/

/-- One iteration of the loop ends the run with the right answer, or advances with the invariant. -/
theorem boundaryStep_correct (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int) (idx : Nat)
    (hwf : p.wf) (hawf : atoms.wf2 p) (hring : 2 ≤ p.ring) (hscan : ScanSound p atoms input)
    (hinv : RunInv p atoms input st prev idx) :
    let r := boundaryStep p atoms input st prev
    (r.2 = none → FinalOK p atoms input r.1) ∧
    (∀ prev', r.2 = some prev' → ∃ idx', idx < idx' ∧ RunInv p atoms input r.1 prev' idx') := by
  intro r
  have hop := opened_of_open p atoms input st prev idx hinv hring
  have hbody : ∀ st1', Opened p atoms input st st1' idx →
      let r' := boundaryBody p atoms input st1' (st.ci % p.ring) prev (liveAt st (st.ci % p.ring) (paGen st.ci))
      (r'.2 = none → FinalOK p atoms input r'.1) ∧
      (∀ prev', r'.2 = some prev' → ∃ idx', idx < idx' ∧ RunInv p atoms input r'.1 prev' idx') := by
    intro st1' hop' r'
    obtain ⟨h1, h2⟩ := body_correct p atoms input st st1' prev idx hwf hawf hring hinv hop'
    exact ⟨h1, fun prev' h => ⟨idx + 1, Nat.lt_succ_self _, h2 prev' h⟩⟩
  simp only [r, boundaryStep, boundaryAfterFilter]
  generalize hst1 : st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci) = st1 at hop
  split
  · rename_i hcond
    simp only [Bool.and_eq_true, List.isEmpty_iff, Bool.not_eq_true', decide_eq_true_eq] at hcond
    obtain ⟨⟨⟨⟨hempty, hm⟩, hen⟩, hposLt⟩, hbol⟩ := hcond
    rw [hop.pos] at hposLt hbol
    rw [hop.matched] at hm
    split
    · rename_i hjump
      rw [hop.pos] at hjump
      refine ⟨fun h => by simp at h, fun prev' h => ?_⟩
      simp only [Option.some.injEq] at h
      subst h
      rw [hop.pos]
      exact skip_inv p atoms input st st1 prev idx hwf hawf hring hscan hinv hop hempty hm hen hposLt hbol hjump _
    · exact hbody _ (Opened_bumpSkipped p atoms input st st1 idx hop _)
  · exact hbody st1 hop

/-- The loop with enough fuel ends with the right answer. -/
theorem paRun_correct (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (hawf : atoms.wf2 p)
    (hring : 2 ≤ p.ring) (hscan : ScanSound p atoms input) :
    ∀ (fuel : Nat) (st : St) (prev : Int) (idx : Nat), RunInv p atoms input st prev idx →
      input.bytes.size + 2 ≤ idx + fuel → FinalOK p atoms input (paRun p atoms input st prev fuel) := by
  intro fuel
  induction fuel with
  | zero =>
    intro st prev idx hinv hfuel
    have := valid_le input idx hinv.valid
    omega
  | succ fuel ih =>
    intro st prev idx hinv hfuel
    obtain ⟨h1, h2⟩ := boundaryStep_correct p atoms input st prev idx hwf hawf hring hscan hinv
    simp only [paRun]
    generalize hr : boundaryStep p atoms input st prev = r at h1 h2
    obtain ⟨st', o⟩ := r
    cases o with
    | none => exact h1 rfl
    | some prev' =>
      obtain ⟨idx', hlt, hinv'⟩ := h2 prev' rfl
      exact ih st' prev' idx' hinv' (by omega)

/-- The zeroed workspace satisfies the run invariant at the first boundary. -/
theorem prepare_inv (p : Prog) (atoms : Atoms) (input : Input) (hring : 2 ≤ p.ring) :
    RunInv p atoms input (prepare p) (-2) 0 := by
  have hslot : ∀ si, si < p.ring → (prepare p).slot si =
      { table := Array.replicate p.n { ctr := List.replicate p.k 0 }, active := [], activeCap := 0, gen := 0 } := by
    intro si hsi
    simp [prepare, St.slot, hsi]
  have hentry : ∀ pc, pc < p.n →
      ({ table := Array.replicate p.n { ctr := List.replicate p.k 0 }, active := [], activeCap := 0, gen := 0 } : Slot).entry pc =
        { ctr := List.replicate p.k 0 } := by
    intro pc hpc
    simp [Slot.entry_eq, hpc]
  have hci : (prepare p).ci = 0 := rfl
  refine ⟨by simp [prepare], rfl, Or.inl rfl, ⟨fun h => by omega, fun h => by omega⟩, ?_, ?_, ?_, by simp [prepare],
    fun h => absurd h Bool.false_ne_true, fun _ _ e _ he => absurd he (Nat.not_lt_zero e)⟩
  · intro d hd
    rw [hci, hslot _ (Nat.mod_lt _ (by omega))]
    exact ⟨by simp, fun pc hpc => by rw [hentry pc hpc]; simp, fun pc hpc => by rw [hentry pc hpc]; simp,
      by show 0 ≤ paGen (0 + d); unfold paGen; omega, fun h => by change 0 = paGen (0 + d) at h; unfold paGen at h; omega⟩
  · intro d hd hg
    rw [hci, hslot _ (Nat.mod_lt _ (by omega))] at hg
    unfold paGen at hg; simp at hg
  · intro d hd T ha hi hp
    exfalso
    obtain ⟨T0, delta, _, hlt, _, _⟩ := ha
    omega

/--
Phase A is correct on every program and subject. When the run reports a match, its start and end, with the
counters it kept, form the earliest-start, minimal-counters, longest-end candidate of the reference; when
it reports none, the reference has no candidate at all. The program must be well-formed, the multi-character
probes must fit the ring, and the scan filter must be sound in the sense of `ScanSound`.
-/
theorem run_correct (p : Prog) (atoms : Atoms) (input : Input) (hwf : p.wf) (hawf : atoms.wf2 p)
    (hring : 2 ≤ p.ring) (hscan : ScanSound p atoms input) :
    let res := run p atoms input
    (res.matched = true → ∃ c, IsBest p atoms input res.so c res.eo) ∧
    (res.matched = false → ∀ s c e, ¬ Cand p atoms input s c e) := by
  intro res
  obtain ⟨h1, h2⟩ := paRun_correct p atoms input hwf hawf hring hscan (input.bytes.size + 2) (prepare p) (-2) 0
    (prepare_inv p atoms input hring) (by omega)
  refine ⟨fun hm => ⟨_, h1 hm, fun s c e hc => (h2 s c e hc).2⟩, fun hm s c e hc => ?_⟩
  have hm' : (paRun p atoms input (prepare p) (-2) (input.bytes.size + 2)).matched = false := hm
  rw [(h2 s c e hc).1] at hm'
  exact Bool.false_ne_true hm'.symm

end PhaseA
end Vego
