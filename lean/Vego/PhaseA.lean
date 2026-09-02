/-
A model of phase A, the lockstep automaton of `engine.go`, with a resource meter.

The model mirrors `runPhaseA` and everything under it function for function: the slot tables with their
generation stamps, the relaxation queue and its compaction, the merge order of payloads, the consuming
transitions with their counter increments, the scan filter, and the early stop.
It is parametric in the compiled program, in the atom tests of the consuming instructions, and in the subject.
The atom tests are abstract because phase A only asks them yes or no questions; their cost enters the meter
as the abstract `atom` figure of the contract.

The meter counts events rather than units directly: boundaries visited, queue pops and pushes, relaxations,
arrivals, compaction work, and so on.
`stepFigure` prices those events the way the interpreter's loop meter prices the Vego code, one unit per
loop iteration and per call, with per-event constants that dominate the code paths.
`Vego/PhaseALink.lean` checks the model, its allocation count and that pricing against the interpreted
engine on the corpus.
`Vego/PhaseAProofs.lean` bounds every counter for every program and subject.

Allocation follows the interpreter's rule: a `make` charges its size, and an append past capacity charges
the grown buffer at `growCap`, the portable rule of the targets.

Everything is written as plain recursion, so the proofs can follow each step.
-/

import Vego.CostLemmas
import Ere.Syntax

namespace Vego
namespace PhaseA

/-- The instruction opcodes of `program.go`. -/
inductive Op where
  | rune | runeFold | any | bracket | bol | eol | split | jmp | accept | fail
  deriving Repr, BEq, DecidableEq, Inhabited

structure Instr where
  op : Op := .fail
  next : Nat := 0
  alt : Nat := 0
  arg : Nat := 0
  /-- The counter slots this consuming instruction increments: the mask bits and the extra list. -/
  slots : List Nat := []
  deriving Repr, Inhabited

/-- The scan filter of a program: the bytes a match can begin on at a mid-subject boundary. -/
structure Scan where
  enabled : Bool := false
  single : Bool := false
  b : UInt8 := 0
  stop : UInt8 → Bool := fun _ => false

structure Prog where
  ins : Array Instr
  start : Nat
  /-- The counter slots, `minSlots`. -/
  k : Nat
  /-- The ring size: 2, or `maxElemAhead + 1` when a bracket can consume several characters. -/
  ring : Nat
  scan : Scan

abbrev Prog.n (p : Prog) : Nat := p.ins.size

/-- The decidable well-formedness the universal theorems assume: the start and every edge target are
instructions, and the scan filter is only enabled with a ring of two. -/
def Prog.wfCheck (p : Prog) : Bool :=
  p.start < p.n && (List.range p.n).all (fun pc => (p.ins.getD pc default).next < p.n && (p.ins.getD pc default).alt < p.n) &&
  (!p.scan.enabled || p.ring == 2)

def maxElemAhead : Nat := 8

/--
The atom tests. `single pc cur` decides whether the consuming instruction `pc` accepts the one character
`cur`, where `-1` is an invalid byte and `-2` the end of the subject. `lens pc` lists the lengths a
multi-character bracket probes, and `multi pc ahead` decides one probe over the lookahead.
-/
structure Atoms where
  single : Nat → Int → Bool
  lens : Nat → List Nat
  multi : Nat → List Int → Bool

structure Input where
  bytes : ByteArray
  notbol : Bool
  noteol : Bool
  nlMode : Bool

/-- `decodeRuneAt`: one character and its size, with `-1` and size one for an invalid byte. -/
def decodeRuneAt (bs : ByteArray) (at_ : Nat) : Int × Nat :=
  match Ere.decodeOne bs at_ with
  | some (c, size) => ((c : Int), size)
  | none => (-1, 1)

/-- The generation stamp of a boundary. Stamps start at one, so zeroed tables never collide. -/
def paGen (boundary : Nat) : Nat := boundary + 1

/-- Lexicographic order on counter vectors, `ctrLess`. -/
def ctrLess : List Nat → List Nat → Bool
  | a :: as, b :: bs => if a != b then decide (a < b) else ctrLess as bs
  | _, _ => false

/-- One instruction's payload in a slot table: its stamp, and the start and counters it holds. -/
structure Entry where
  stamp : Nat := 0
  start : Nat := 0
  ctr : List Nat := []
  deriving Inhabited, Repr

/-- One boundary's slot table. `active` is in push order, and `activeCap` is its capacity for the growth rule. -/
structure Slot where
  table : Array Entry
  active : List Nat
  activeCap : Nat
  gen : Nat
  deriving Inhabited

/-- The event counters. -/
structure Meter where
  allocBytes : Nat := 0
  boundaries : Nat := 0
  skipped : Nat := 0
  filter : Nat := 0
  pops : Nat := 0
  pushes : Nat := 0
  compactWork : Nat := 0
  relaxes : Nat := 0
  considers : Nat := 0
  tests : Nat := 0
  arrivals : Nat := 0
  aheadWork : Nat := 0
  pending : Nat := 0
  deriving Repr, Inhabited

/--
The executor state: the workspace of `engineWS` and the scalars of `phaseAState`.
`queue` holds the relaxation queue with its top first, so a push is a cons and a pop takes the head.
-/
structure St where
  slots : Array Slot
  queue : List Nat
  queueCap : Nat
  bestCtr : List Nat
  ahead : List Int
  aheadCap : Nat
  matched : Bool := false
  so : Nat := 0
  eo : Nat := 0
  ci : Nat := 0
  pos : Nat := 0
  bol : Bool := false
  eol : Bool := false
  cur : Int := -2
  m : Meter := {}
  deriving Inhabited

/-- The bytes one append charges: nothing inside capacity, else the grown buffer at four bytes per element. -/
def growBytes (len cap : Nat) : Nat :=
  if len + 1 ≤ cap then 0 else 4 * growCap cap (len + 1)

/-- The capacity after one append. -/
def growCapAfter (len cap : Nat) : Nat :=
  if len + 1 ≤ cap then cap else growCap cap (len + 1)

/-- The bytes `prepare` allocates, at the shared 64-bit layout: a `slotTable` is 104 bytes. -/
def prepareBytes (n k ring : Nat) : Nat :=
  ring * 104 + ring * (4 * n + 4 * n + (if k > 0 then 4 * n * k else 0)) +
    n + (if k > 0 then 12 * k else 0) + 64

/-- `prepare`: the zeroed workspace. -/
def prepare (p : Prog) : St :=
  let slot : Slot :=
    { table := Array.replicate p.n { ctr := List.replicate p.k 0 }, active := [], activeCap := 0, gen := 0 }
  { slots := Array.replicate p.ring slot, queue := [], queueCap := 16,
    bestCtr := List.replicate p.k 0, ahead := [], aheadCap := 0,
    m := { allocBytes := prepareBytes p.n p.k p.ring } }

def St.charge (st : St) (bytes : Nat) : St :=
  { st with m := { st.m with allocBytes := st.m.allocBytes + bytes } }

def St.slot (st : St) (si : Nat) : Slot := st.slots[si]?.getD default

def St.setSlot (st : St) (si : Nat) (sl : Slot) : St :=
  { st with slots := st.slots.setIfInBounds si sl }

def Slot.entry (sl : Slot) (pc : Nat) : Entry := sl.table[pc]?.getD default

/-- The candidate update of `paConsider`: earliest start, then smallest counters, then longest end. -/
def considerCore (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (end_ : Nat) : St :=
  if !st.matched || start < st.so then
    { st with matched := true, so := start, eo := end_, bestCtr := ctr }
  else if start > st.so then st
  else if k > 0 && ctrLess ctr st.bestCtr then { st with eo := end_, bestCtr := ctr }
  else if k > 0 && ctrLess st.bestCtr ctr then st
  else if end_ > st.eo then { st with eo := end_ }
  else st

/-- `paConsider`: record a match candidate. -/
def paConsider (k : Nat) (st : St) (start : Nat) (ctr : List Nat) (end_ : Nat) : St :=
  considerCore k { st with m := { st.m with considers := st.m.considers + 1 } } start ctr end_

/-- `paPrune`: a payload that can no longer beat the best candidate. -/
def paPrune (k : Nat) (st : St) (start : Nat) (ctr : List Nat) : Bool :=
  if !st.matched then false
  else if start > st.so then true
  else if start < st.so || k == 0 then false
  else ctrLess st.bestCtr ctr

/-- A payload strictly better than a stored one: an earlier start, or the same start and smaller counters. -/
def betterThan (k : Nat) (start : Nat) (ctr : List Nat) (e : Entry) : Bool :=
  start < e.start || (start == e.start && k != 0 && ctrLess ctr e.ctr)

/-- Does the slot take this payload at `pc`: a stale entry always does, a fresh one only for a better payload. -/
def acceptsStore (k : Nat) (sl : Slot) (pc : Nat) (start : Nat) (ctr : List Nat) : Bool :=
  (sl.entry pc).stamp != sl.gen || betterThan k start ctr (sl.entry pc)

/-- Write a payload into a slot at `pc`. A stale entry joins the active list, and the bytes that grows are reported. -/
def storeInto (sl : Slot) (pc : Nat) (start : Nat) (ctr : List Nat) : Slot × Nat :=
  if (sl.entry pc).stamp == sl.gen then
    ({ sl with table := sl.table.setIfInBounds pc { sl.entry pc with start, ctr } }, 0)
  else
    ({ sl with table := sl.table.setIfInBounds pc { stamp := sl.gen, start, ctr },
               active := sl.active ++ [pc],
               activeCap := growCapAfter sl.active.length sl.activeCap },
     growBytes sl.active.length sl.activeCap)

/-- `paStore`: merge a payload into slot `si` at `pc`. It reports whether the payload stayed. -/
def paStore (k : Nat) (st : St) (si pc : Nat) (start : Nat) (ctr : List Nat) : Bool × St :=
  if paPrune k st start ctr then (false, st)
  else if acceptsStore k (st.slot si) pc start ctr then
    let (sl, bytes) := storeInto (st.slot si) pc start ctr
    (true, (st.setSlot si sl).charge bytes)
  else (false, st)

/-- Push onto the relaxation queue, charging any growth. -/
def St.pushQueue (st : St) (pc : Nat) : St :=
  ({ st with queue := pc :: st.queue, queueCap := growCapAfter st.queue.length st.queueCap,
             m := { st.m with pushes := st.m.pushes + 1 } }).charge (growBytes st.queue.length st.queueCap)

/-- `paRelax`: merge at the current boundary and queue for the closure. -/
def paRelax (k : Nat) (st : St) (si pc : Nat) (start : Nat) (ctr : List Nat) : St :=
  let st := { st with m := { st.m with relaxes := st.m.relaxes + 1 } }
  let (stayed, st) := paStore k st si pc start ctr
  if stayed then st.pushQueue pc else st

def queueCompactFactor : Nat := 2

/-- Keep the first occurrence of every element. The fuel is the list length, which bounds the recursion. -/
def dedupFirstN : Nat → List Nat → List Nat
  | 0, _ => []
  | _, [] => []
  | fuel + 1, x :: xs => x :: dedupFirstN fuel (xs.filter (· != x))

def dedupFirst (l : List Nat) : List Nat := dedupFirstN l.length l

/-- `compactQueue`: keep the first occurrence of every instruction in push order. -/
def compactQueue (st : St) : St :=
  { st with queue := (dedupFirst st.queue.reverse).reverse,
            m := { st.m with compactWork := st.m.compactWork + 2 * st.queue.length + 3 } }

/-- Pop the head of the queue, leaving `rest`. -/
def St.popQueue (st : St) (rest : List Nat) : St :=
  { st with queue := rest, m := { st.m with pops := st.m.pops + 1 } }

/-- The epsilon transitions of one popped instruction, with the payload it held. -/
def handleOp (p : Prog) (si : Nat) (st : St) (pc : Nat) (start : Nat) (ctr : List Nat) : St :=
  match (p.ins.getD pc default).op with
  | .split => paRelax p.k (paRelax p.k st si (p.ins.getD pc default).next start ctr) si
      (p.ins.getD pc default).alt start ctr
  | .jmp => paRelax p.k st si (p.ins.getD pc default).next start ctr
  | .bol => if st.bol then paRelax p.k st si (p.ins.getD pc default).next start ctr else st
  | .eol => if st.eol then paRelax p.k st si (p.ins.getD pc default).next start ctr else st
  | .accept => paConsider p.k st start ctr st.pos
  | _ => st

/-- One popped instruction: a stale entry is skipped, a fresh one is handled with its payload. -/
def handle (p : Prog) (si : Nat) (st : St) (pc : Nat) : St :=
  if ((st.slot si).entry pc).stamp != (st.slot si).gen then st
  else handleOp p si st pc ((st.slot si).entry pc).start ((st.slot si).entry pc).ctr

/-- Pop and handle the top of the queue. -/
def drain (p : Prog) (si : Nat) (st : St) : St :=
  match st.queue with
  | [] => st
  | pc :: rest => handle p si (st.popQueue rest) pc

/-- One drain step of `paClosure`: compaction past the threshold, then one pop. -/
def closureStep (p : Prog) (st : St) (si : Nat) : St :=
  drain p si (if st.queue.length > queueCompactFactor * p.n then compactQueue st else st)

/-- `paClosure`: drain the queue. The fuel is a bound the proofs justify, so it never binds. -/
def paClosure (p : Prog) (si : Nat) (st : St) : Nat → St
  | 0 => st
  | fuel + 1 => if st.queue.isEmpty then st else paClosure p si (closureStep p st si) fuel

def closureFuel (p : Prog) (st : St) : Nat := st.queue.length + (p.n + 1) * (p.n + 1) + 1

/-- Add `delta` to the counters an instruction's slots select. -/
def bumpCtr (ctr : List Nat) (slots : List Nat) (delta : Nat) : List Nat :=
  ctr.zipIdx.map fun (c, i) => if slots.contains i then c + delta else c

/-- `paArrive`: file a consuming transition into a future boundary. -/
def paArrive (p : Prog) (st : St) (pc delta : Nat) (start : Nat) (ctr : List Nat) : St :=
  let st := { st with m := { st.m with arrivals := st.m.arrivals + 1 } }
  let fi := (st.ci + delta) % p.ring
  let g := paGen (st.ci + delta)
  let st := if (st.slot fi).gen != g then st.setSlot fi { st.slot fi with gen := g, active := [] } else st
  let ins := p.ins.getD pc default
  let newCtr := if p.k > 0 && !ins.slots.isEmpty then bumpCtr ctr ins.slots delta else ctr
  (paStore p.k st fi ins.next start newCtr).2

/-- Append one character to the lookahead, charging any growth and the decode work. -/
def St.pushAhead (st : St) (r : Int) : St :=
  ({ st with ahead := st.ahead ++ [r], aheadCap := growCapAfter st.ahead.length st.aheadCap,
             m := { st.m with aheadWork := st.m.aheadWork + 2 } }).charge (growBytes st.ahead.length st.aheadCap)

/-- `decodeAhead`: up to `maxElemAhead` characters from `at_`. -/
def decodeAheadFrom (bytes : ByteArray) (at_ : Nat) (st : St) : Nat → St
  | 0 => st
  | fuel + 1 =>
    if at_ < bytes.size then
      let (r, size) := decodeRuneAt bytes at_
      decodeAheadFrom bytes (at_ + size) (st.pushAhead r) fuel
    else st

/-- Clear the lookahead and pay for the call. -/
def St.resetAhead (st : St) : St :=
  { st with ahead := [], m := { st.m with aheadWork := st.m.aheadWork + 2 } }

def decodeAhead (input : Input) (st : St) : St :=
  decodeAheadFrom input.bytes st.pos st.resetAhead maxElemAhead

/-- The multi-character probes of one bracket instruction. -/
def probeLens (p : Prog) (atoms : Atoms) (st : St) (pc : Nat) (start : Nat) (ctr : List Nat) :
    List Nat → St
  | [] => st
  | len :: rest =>
    let st := if len ≤ st.ahead.length && atoms.multi pc (st.ahead.take len) then paArrive p st pc len start ctr
              else st
    probeLens p atoms st pc start ctr rest

/-- Count one visit of a live instruction. -/
def St.bumpTests (st : St) : St := { st with m := { st.m with tests := st.m.tests + 1 } }

/-- The single-character transition of one live instruction. -/
def consumeArrive (p : Prog) (atoms : Atoms) (st : St) (pc start : Nat) (ctr : List Nat) : St :=
  if atoms.single pc st.cur then paArrive p st pc 1 start ctr else st

/-- The multi-character probes of a bracket, decoding the lookahead first when it is not ready. -/
def consumeProbe (p : Prog) (atoms : Atoms) (input : Input) (st : St) (aheadReady : Bool) (pc start : Nat)
    (ctr : List Nat) : St × Bool :=
  if (p.ins.getD pc default).op == .bracket && !(atoms.lens pc).isEmpty then
    (probeLens p atoms (if aheadReady then st else decodeAhead input st) pc start ctr (atoms.lens pc), true)
  else (st, aheadReady)

/-- A fresh live instruction with its payload. -/
def consumeFresh (p : Prog) (atoms : Atoms) (input : Input) (st : St) (aheadReady : Bool) (pc start : Nat)
    (ctr : List Nat) : St × Bool :=
  match (p.ins.getD pc default).op with
  | .rune | .runeFold | .any | .bracket =>
    consumeProbe p atoms input (consumeArrive p atoms st pc start ctr) aheadReady pc start ctr
  | _ => (st, aheadReady)

/-- `paConsume` over one live instruction. `aheadReady` says whether the lookahead is decoded. -/
def consumeOne (p : Prog) (atoms : Atoms) (input : Input) (si : Nat) (st : St) (aheadReady : Bool)
    (pc : Nat) : St × Bool :=
  if ((st.slot si).entry pc).stamp != (st.slot si).gen then (st.bumpTests, aheadReady)
  else consumeFresh p atoms input st.bumpTests aheadReady pc ((st.slot si).entry pc).start
    ((st.slot si).entry pc).ctr

def consumeList (p : Prog) (atoms : Atoms) (input : Input) (si : Nat) :
    List Nat → St → Bool → St
  | [], st, _ => st
  | pc :: rest, st, aheadReady =>
    let (st, aheadReady) := consumeOne p atoms input si st aheadReady pc
    consumeList p atoms input si rest st aheadReady

/-- `paConsume`: every consuming instruction over the current character or collating element. -/
def paConsume (p : Prog) (atoms : Atoms) (input : Input) (st : St) (si : Nat) : St :=
  consumeList p atoms input si (st.slot si).active st false

/-- `scanAhead`: the next stop byte at or after `i`, or the subject length, with the bytes scanned. -/
def scanAheadFrom (p : Prog) (input : Input) (i : Nat) : Nat → Nat × Nat
  | 0 => (i, 0)
  | fuel + 1 =>
    if i < input.bytes.size then
      let b := input.bytes.get! i
      if (p.scan.single && b == p.scan.b) || (!p.scan.single && p.scan.stop b) then (i, 1)
      else
        let (r, scanned) := scanAheadFrom p input (i + 1) fuel
        (r, scanned + 1)
    else (i, 0)

def scanAhead (p : Prog) (input : Input) (pos : Nat) : Nat × Nat :=
  scanAheadFrom p input pos (input.bytes.size + 1)

def bolAt (input : Input) (pos : Nat) (prev : Int) : Bool :=
  (pos == 0 && !input.notbol) || (input.nlMode && prev == 10)

/-- The early-stop test: is any future boundary live? Returns the answer and the slots examined. -/
def pendingFrom (p : Prog) (st : St) (delta : Nat) : Nat → Bool × Nat
  | 0 => (false, 0)
  | fuel + 1 =>
    if delta < p.ring then
      let fsl := st.slot ((st.ci + delta) % p.ring)
      if fsl.gen == paGen (st.ci + delta) && !fsl.active.isEmpty then (true, 1)
      else
        let (r, c) := pendingFrom p st (delta + 1) fuel
        (r, c + 1)
    else (false, 0)

/-! The pieces of one boundary, each a named step so that the proofs can follow them. -/

/-- The character at the boundary and the anchor flags. -/
def St.setFlags (st : St) (input : Input) (prev : Int) : St :=
  { st with cur := if st.pos == input.bytes.size then (-2 : Int) else (decodeRuneAt input.bytes st.pos).1,
            bol := bolAt input st.pos prev,
            eol := (st.pos == input.bytes.size && !input.noteol) ||
              (input.nlMode && (if st.pos == input.bytes.size then (-2 : Int) else (decodeRuneAt input.bytes st.pos).1) == 10) }

/-- `append(queue[:0], active...)`: the live list becomes the queue, growing it when its capacity is short. -/
def St.copyQueue (st : St) (active : List Nat) : St :=
  if active.length > st.queueCap then
    ({ st with queue := active.reverse, queueCap := growCap st.queueCap active.length }).charge
      (4 * growCap st.queueCap active.length)
  else { st with queue := active.reverse }

/-- Spawn a thread at the start instruction while no match is known. -/
def spawn (p : Prog) (st : St) (si : Nat) : St :=
  if st.matched then st else paRelax p.k st si p.start st.pos (List.replicate p.k 0)

/-- Drain the closure with the fuel the proofs justify. -/
def closeAt (p : Prog) (si : Nat) (st : St) : St := paClosure p si st (closureFuel p st)

def St.advance (st : St) (size : Nat) : St := { st with pos := st.pos + size, ci := st.ci + 1 }

def St.bumpPending (st : St) (count : Nat) : St := { st with m := { st.m with pending := st.m.pending + count } }

/-- After the consuming transitions: stop when a match is known and nothing is in flight. -/
def afterConsume (p : Prog) (st : St) (size : Nat) : St × Option Int :=
  if st.matched then
    if (pendingFrom p st 1 p.ring).1 then
      ((st.bumpPending (pendingFrom p st 1 p.ring).2).advance size, some st.cur)
    else (st.bumpPending (pendingFrom p st 1 p.ring).2, none)
  else (st.advance size, some st.cur)

/-- The boundary's work after the scan filter: the closure, the consumption, and the stop test. -/
def boundaryBody (p : Prog) (atoms : Atoms) (input : Input) (st : St) (si : Nat) (prev : Int)
    (active : List Nat) : St × Option Int :=
  if st.pos == input.bytes.size then
    (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si), none)
  else
    afterConsume p (paConsume p atoms input (closeAt p si (spawn p ((st.setFlags input prev).copyQueue active) si)) si)
      (decodeRuneAt input.bytes st.pos).2

/-- The live instructions of slot `si` at generation `g`. -/
def liveAt (st : St) (si g : Nat) : List Nat :=
  (st.slot si).active.filter fun pc => ((st.slot si).entry pc).stamp == g

/-- Open a boundary: stamp the slot with the new generation and keep its live instructions. -/
def St.filterSlot (st : St) (si g : Nat) : St :=
  { st.setSlot si { st.slot si with gen := g, active := liveAt st si g } with
    m := { st.m with filter := st.m.filter + (st.slot si).active.length } }

def St.bumpBoundaries (st : St) : St := { st with m := { st.m with boundaries := st.m.boundaries + 1 } }
def St.bumpSkipped (st : St) (scanned : Nat) : St := { st with m := { st.m with skipped := st.m.skipped + scanned } }
def St.jumpTo (st : St) (next : Nat) : St := { st with pos := next, ci := st.ci + 1 }

/-- The previous character after a jump of the scan filter: a newline is what the anchors need to know. -/
def prevAfterJump (input : Input) (next : Nat) : Int :=
  if next > 0 && input.bytes.get! (next - 1) == 10 then 10 else 120

/-- The boundary after its slot was opened: the scan filter, then the body. -/
def boundaryAfterFilter (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int) (active : List Nat)
    (si : Nat) : St × Option Int :=
  if active.isEmpty && !st.matched && p.scan.enabled && st.pos < input.bytes.size &&
      !bolAt input st.pos prev then
    if (scanAhead p input st.pos).1 > st.pos then
      ((st.bumpSkipped (scanAhead p input st.pos).2).jumpTo (scanAhead p input st.pos).1,
       some (prevAfterJump input (scanAhead p input st.pos).1))
    else boundaryBody p atoms input (st.bumpSkipped (scanAhead p input st.pos).2) si prev active
  else boundaryBody p atoms input st si prev active

/-- One iteration of the `paRun` loop: the state after it, and the previous character for the next one, or `none` when the run is over. -/
def boundaryStep (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int) :
    St × Option Int :=
  boundaryAfterFilter p atoms input (st.bumpBoundaries.filterSlot (st.ci % p.ring) (paGen st.ci)) prev
    (liveAt st (st.ci % p.ring) (paGen st.ci)) (st.ci % p.ring)

/-- `paRun`: the boundary loop, with the fuel the proofs justify. -/
def paRun (p : Prog) (atoms : Atoms) (input : Input) (st : St) (prev : Int) : Nat → St
  | 0 => st
  | fuel + 1 =>
    match boundaryStep p atoms input st prev with
    | (st', none) => st'
    | (st', some prev') => paRun p atoms input st' prev' fuel

structure Result where
  matched : Bool
  so : Nat
  eo : Nat
  m : Meter
  deriving Repr

/-- `runPhaseA`: prepare, then scan. -/
def run (p : Prog) (atoms : Atoms) (input : Input) : Result :=
  let st := paRun p atoms input (prepare p) (-2) (input.bytes.size + 2)
  { matched := st.matched, so := st.so, eo := st.eo, m := st.m }

/-!
## The step figure

One unit per loop iteration and per call, as the interpreter's loop meter counts them.
The constants per event dominate the code path of the event; `PhaseALink.lean` checks that on the corpus.
-/

/-- The price of the events of one run, given the counter slots, the ring, and the atom figure. -/
def stepFigure (m : Meter) (k ring atom : Nat) : Nat :=
  24 + ring +
  m.boundaries * (14 + 2 * ring) +
  m.skipped + m.filter + m.pops + m.compactWork +
  m.relaxes * (2 * k + 7) +
  m.considers * (2 * k + 5) +
  m.tests * (atom + 2) +
  m.arrivals * (4 * k + 12) +
  m.aheadWork + m.pending * 2

/-!
## The contract figures the proofs establish

`weight` pays for one unit of the closure measure: a pop, two relaxations, and compaction slack.
`perTest` prices one live instruction: its test, its arrivals, and the lookahead when it is first needed.
`perBoundary` prices one boundary, and `stepsFigure` one run on a subject of `len` bytes.
`heapFigure` bounds the bytes one run allocates.
`Vego/PhaseAProofs.lean` proves the two bounds for every program, atom test and subject.
-/

def weight (k : Nat) : Nat := 4 * k + 22

def perTest (p : Prog) (atom : Nat) : Nat := atom + 2 + (p.ring - 1) * (4 * p.k + 12)

def perBoundary (p : Prog) (atom : Nat) : Nat :=
  weight p.k * (p.n + 1) * (p.n + 1) + p.n * perTest p atom + p.n + 2 * p.k + 4 * p.ring + 38

def stepsFigure (p : Prog) (atom len : Nat) : Nat := 24 + p.ring + len + (len + 1) * perBoundary p atom

def heapFigure (n k ring : Nat) : Nat := prepareBytes n k ring + ring * (16 * n + 64) + 32 * n + 272

end PhaseA
end Vego
