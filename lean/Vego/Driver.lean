/-
The cross-language driver protocol, instantiated over the interpreted revera engine.

This is the Lean counterpart of dev/internal/protocol/driver.go.
One session holds the base locale table, the selected locale, and the compiled pattern.
Every protocol command maps to calls into the checked program.
The crosscheck corpus, evaluated by the Go original, gives the expected output for every command.
The corpus theorem states that this session reproduces those outputs exactly.
-/

import Vego.Machine
import Vego.Data

namespace Vego

private def hexVal (b : UInt8) : Option Nat :=
  if 48 ≤ b && b ≤ 57 then some (b.toNat - 48)
  else if 97 ≤ b && b ≤ 102 then some (b.toNat - 87)
  else if 65 ≤ b && b ≤ 70 then some (b.toNat - 55)
  else none

def hexDecode (s : String) : Option ByteArray := Id.run do
  let bs := s.toUTF8
  if bs.size % 2 != 0 then return none
  let mut out := ByteArray.emptyWithCapacity (bs.size / 2)
  let mut i := 0
  while h : i < bs.size do
    match hexVal bs[i], hexVal bs[i + 1]! with
    | some x, some y => out := out.push (UInt8.ofNat (x * 16 + y))
    | _, _ => return none
    i := i + 2
  return some out

def hexEncode (b : ByteArray) : String := Id.run do
  let mut out := ""
  for byte in b do
    out := out.push (Nat.digitChar (byte.toNat / 16))
    out := out.push (Nat.digitChar (byte.toNat % 16))
  return out

/-- The embedded locale table, the same bytes as data.bin. -/
def localeData : ByteArray :=
  (hexDecode (include_str "../data/localedata.hex").trim).getD
    ByteArray.empty

/-- Protocol token codecs: "-" stands for the empty string. -/
def tokDecode (t : String) : Option ByteArray :=
  if t == "-" then some ByteArray.empty else hexDecode t

def tokEncode (b : ByteArray) : String :=
  if b.size == 0 then "-" else hexEncode b

/-
Heap compaction.
A long driver session allocates buffers that nothing references once a command finishes.
This moves the session roots into a fresh heap and drops the rest.
Sharing between headers is kept through the memo table.
-/
private def migratedGeneration (old : Array Cell) (obj gen : Nat) : Nat :=
  match old[obj]? with
  | some (oldGen, _) => if oldGen == gen then 0 else 1
  | none => 1

mutual

partial def migrateVal (old : Array Cell) (memo : Array (Option Nat))
    (nh : Array Cell) (v : Val) : (Val × Array (Option Nat) × Array Cell) :=
  match v with
  | .i _ | .b _ | .s _ => (v, memo, nh)
  | .slice none _ _ _ => (v, memo, nh)
  | .slice (some (obj, gen, path)) off len cap =>
    let (obj', memo, nh) := migrateCell old memo nh obj
    (.slice (some (obj', migratedGeneration old obj gen, path)) off len cap,
     memo, nh)
  | .ptr obj gen path =>
    let (obj', memo, nh) := migrateCell old memo nh obj
    (.ptr obj' (migratedGeneration old obj gen) path, memo, nh)
  | .arr es =>
    let (es', memo, nh) := migrateAll old memo nh es
    (.arr es', memo, nh)
  | .strukt fs =>
    let (fs', memo, nh) := migrateAll old memo nh fs
    (.strukt fs', memo, nh)

partial def migrateAll (old : Array Cell) (memo : Array (Option Nat))
    (nh : Array Cell) (es : Array Val) :
    (Array Val × Array (Option Nat) × Array Cell) := Id.run do
  let mut memo := memo
  let mut nh := nh
  let mut out : Array Val := #[]
  for e in es do
    let (e', memo', nh') := migrateVal old memo nh e
    memo := memo'
    nh := nh'
    out := out.push e'
  return (out, memo, nh)

partial def migrateCell (old : Array Cell) (memo : Array (Option Nat))
    (nh : Array Cell) (obj : Nat) : (Nat × Array (Option Nat) × Array Cell) :=
  match memo.getD obj none with
  | some idx => (idx, memo, nh)
  | none =>
    let idx := nh.size
    let memo := memo.setIfInBounds obj (some idx)
    let nh := nh.push (0, .b false)
    let (v', memo, nh) :=
      migrateVal old memo nh (old.getD obj (0, .b false)).2
    (idx, memo, nh.set! idx (0, v'))

end

/--
Session errors.
A run out of fuel is recoverable and deterministic, and everything else is a hard fault.
-/
inductive DriverErr where
  | outOfFuel
  | fault (msg : String)

/-- Function and field positions the protocol needs, resolved once at session start, plus the zero Match value. -/
structure SessionIds where
  localePOSIX : Nat
  localeSelect : Nat
  compile : Nat
  numSub : Nat
  exec : Nat
  replaceAll : Nat
  matchIterInit : Nat
  matchIterNext : Nat
  contractFor : Nat
  contractHeapBytes : Nat
  contractStackBytes : Nat
  contractSteps : Nat
  localeToUpper : Nat
  localeToLower : Nat
  errCode : Nat
  errPos : Nat
  matchSo : Nat
  matchEo : Nat
  contractHasSolver : Nat
  zeroMatch : Val

/--
One interpreted call frame, in bytes, as the resource contract estimates it.
Mirrors frameBytes in contract.go.
-/
def frameBytes : Nat := 256

/-- One Exec call measured against the pattern's contract: what the meter saw, next to the figures the engine's own contract code reported for this subject length. -/
structure MeterStat where
  heapUsed : Nat
  heapBound : Int
  depthUsed : Nat
  stackBound : Int
  stepsUsed : Nat
  loopsUsed : Nat
  stepsBound : Int
  deriving Repr, Inhabited

/--
One live driver session.
`fuel` bounds the recursion depth of every engine call, not its aggregate work.
The default never binds in practice.
`calibrate` swaps enforcement for measurement.
An Exec over its contract normally fails the session.
A calibrating session records the figures instead, which is what the vegocheck margin reports read.
-/
structure Session where
  m : Machine
  ids : SessionIds
  baseCell : Nat
  curCell : Nat
  reCell : Nat
  valid : Bool
  fuel : Nat := Machine.defaultFuel
  calibrate : Bool := false
  stats : Array MeterStat := #[]
  /-
  Contract figures per argument, for the current pattern.
  Only Compile writes the pattern, so between two C commands the contract is a function of the argument alone.
  Evaluating ContractFor costs several AST walks, so each compile starts a fresh cache.
  -/
  conCache : List (Int × (Bool × Int × Int × Int)) := []

namespace Session

abbrev SR := Except DriverErr

instance : MonadLift (Except String) SR where
  monadLift x := x.mapError .fault

private def trap {α : Type} (x : Except Trap α) (what : String) : SR α :=
  match x with
  | .ok v => pure v
  | .error .fuel => throw .outOfFuel
  | .error t => throw (.fault s!"{what}: trap {repr t}")

private def die {α : Type} (msg : String) : SR α := throw (.fault msg)

/--
Engine calls under the session's fuel bound, by resolved index.
`what` only labels errors.
-/
private def call1 (s : Session) (idx : Nat) (what : String)
    (args : List Val) : SR (Val × Session) := do
  match ← trap (s.m.callIdx idx args s.fuel) what with
  | ([v], m) => pure (v, { s with m })
  | _ => die s!"{what} arity"

private def call2 (s : Session) (idx : Nat) (what : String)
    (args : List Val) : SR (Val × Val × Session) := do
  match ← trap (s.m.callIdx idx args s.fuel) what with
  | ([v1, v2], m) => pure (v1, v2, { s with m })
  | _ => die s!"{what} arity"

private def resolveIds (m : Machine) : Except String SessionIds := do
  let fn (n : String) : Except String Nat :=
    match m.fnIdx? n with
    | some i => pure i
    | none => throw s!"no function {n}"
  let fld (st f : String) : Except String Nat := do
    let some si := m.structIdx? st | throw s!"no struct {st}"
    let some fields := m.prog.structFields[si]? | throw "bad struct index"
    let some i := fields.idxOf? f | throw s!"no field {st}.{f}"
    pure i
  pure { localePOSIX := ← fn "LocalePOSIX"
         localeSelect := ← fn "LocaleSelect"
         compile := ← fn "Compile"
         numSub := ← fn "NumSub"
         exec := ← fn "Exec"
         replaceAll := ← fn "ReplaceAll"
         matchIterInit := ← fn "MatchIterInit"
         matchIterNext := ← fn "MatchIterNext"
         contractFor := ← fn "ContractFor"
         contractHeapBytes := ← fn "ContractHeapBytes"
         contractStackBytes := ← fn "ContractStackBytes"
         contractSteps := ← fn "ContractSteps"
         localeToUpper := ← fn "localeToUpper"
         localeToLower := ← fn "localeToLower"
         errCode := ← fld "Error" "Code"
         errPos := ← fld "Error" "Pos"
         matchSo := ← fld "Match" "So"
         matchEo := ← fld "Match" "Eo"
         contractHasSolver := ← fld "Contract" "HasSolver"
         zeroMatch := m.zeroStruct "Match" }

/-- Start a session: load the locale blob and select POSIX. -/
def start (tp : TProgram) : SR Session := do
  let m ← trap (Machine.init tp) "init"
  let ids ← (resolveIds m : Except String SessionIds)
  let ([base, _ok], m) ← trap (m.call "LocaleLoad" [.s localeData]) "LocaleLoad"
    | die "LocaleLoad arity"
  let ([posix], m) ← trap (m.callIdx ids.localePOSIX []) "LocalePOSIX"
    | die "LocalePOSIX arity"
  let (m, baseCell) := m.alloc base
  let (m, curCell) := m.alloc posix
  let (m, reCell) := m.alloc (.b false)
  pure { m, ids, baseCell, curCell, reCell, valid := false }

/-- Compact the heap when it has grown, keeping the session roots. -/
def compact (s : Session) : Session :=
  if s.m.heap.cells.size < 65536 then s
  else
    let old := s.m.heap.cells
    let memo : Array (Option Nat) := Array.replicate old.size none
    let dummy : Array Cell := #[(0, .b false)]
    let (base', memo, nh) := migrateCell old memo dummy s.baseCell
    let (cur', memo, nh) := migrateCell old memo nh s.curCell
    let (re', _, nh) := migrateCell old memo nh s.reCell
    -- Keep the meter.
    -- A fresh Heap literal would reset the counters to their defaults, and compaction must not erase a measurement.
    { s with m := { s.m with
                    heap := { s.m.heap with cells := nh, free := #[] } },
             baseCell := base', curCell := cur', reCell := re' }

private def asI (v : Val) : SR Int :=
  match v with
  | .i n => pure n
  | _ => die "expected an integer"

private def asB (v : Val) : SR Bool :=
  match v with
  | .b x => pure x
  | _ => die "expected a bool"

private def asS (v : Val) : SR ByteArray :=
  match v with
  | .s x => pure x
  | _ => die "expected a string"

/-- Read an Error value as its code and position. -/
private def errParts (s : Session) (v : Val) : SR (Int × Int) := do
  match v with
  | .strukt fs =>
    pure (← asI (fs.getD s.ids.errCode (.b false)),
          ← asI (fs.getD s.ids.errPos (.b false)))
  | _ => die "expected an Error value"

private def numSub (s : Session) : SR (Int × Session) := do
  let (n, s) ← s.call1 s.ids.numSub "NumSub" [.ptr s.reCell 0 []]
  pure (← asI n, s)

/-- Build a zeroed pmatch buffer with n entries. -/
private def mkPmatch (s : Session) (n : Nat) : SR (Val × Session) := do
  let (m, v) := s.m.mkSlice (Array.replicate n s.ids.zeroMatch)
  pure (v, { s with m })

/-- Read back the So,Eo pairs of a pmatch buffer. -/
private def pmatchPairs (s : Session) (pm : Val) : SR (List (Int × Int)) := do
  let some elems := s.m.sliceElems pm | die "unreadable pmatch"
  let mut out : Array (Int × Int) := #[]
  for e in elems do
    match e with
    | .strukt fs => do
      let so ← asI (fs.getD s.ids.matchSo (.b false))
      let eo ← asI (fs.getD s.ids.matchEo (.b false))
      out := out.push (so, eo)
    | _ => die "pmatch holds a non-struct"
  pure out.toList

private def intTok (t : String) : SR Int :=
  match t.toInt? with
  | some v => pure v
  | none => die s!"bad integer token {t}"

private def strTok (t : String) : SR ByteArray :=
  match tokDecode t with
  | some b => pure b
  | none => die s!"bad hex token {t}"

/--
The contract of the compiled pattern, for subjects of at most maxInput bytes.
It reports whether a solver backend can run, then the heap, stack and step figures.
Everything comes from the engine's own contract code under the formal semantics, through the same accessors the cross-language drivers call.

The result is cached per argument.
Only Compile writes the pattern.
Between two C commands the contract is a function of the argument alone.
The T command and the per-Exec check therefore read the same cached figures.
-/
private def contractOf (s : Session) (maxInput : Int) :
    SR ((Bool × Int × Int × Int) × Session) := do
  match s.conCache.lookup maxInput with
  | some c => pure (c, s)
  | none => do
    let (con, s) ← s.call1 s.ids.contractFor "ContractFor"
      [.ptr s.reCell 0 [], .i maxInput]
    let hs ← match con with
      | .strukt fs => asB (fs.getD s.ids.contractHasSolver (.b false))
      | _ => die "expected a Contract value"
    let (m, conCell) := s.m.alloc con
    let s := { s with m }
    let get1 (st : Session) (idx : Nat) (what : String) :
        SR (Int × Session) := do
      let (v, st) ← st.call1 idx what [.ptr conCell 0 []]
      pure (← asI v, st)
    let (heapB, s) ← get1 s s.ids.contractHeapBytes "ContractHeapBytes"
    let (stackB, s) ← get1 s s.ids.contractStackBytes "ContractStackBytes"
    let (stepsB, s) ← get1 s s.ids.contractSteps "ContractSteps"
    let c := (hs, heapB, stackB, stepsB)
    pure (c, { s with conCache := (maxInput, c) :: s.conCache })

/--
Compare one metered Exec against its contract.
The heap meter counts every buffer byte the call allocated, which is what the arena-backed targets consume.
The stack meter is the deepest call chain, priced at the contract's per-frame estimate.
The step comparison uses the loop counter, which ticks once per loop iteration and once per call.
That is the granularity the abstract operations of the contract describe.
The program text bounds the straight-line code between two of those units.
The loop counter therefore bounds the whole work, up to a constant of the artifact.
Exceeding any bound is a hard fault, so the corpus theorem fails if one real execution ever passes its contract.
-/
private def meterCheck (s : Session) (heapB stackB stepsB : Int) :
    SR Session := do
  let h := s.m.heap
  let st : MeterStat :=
    { heapUsed := h.allocBytes, heapBound := heapB,
      depthUsed := h.maxDepth, stackBound := stackB,
      stepsUsed := h.steps, loopsUsed := h.loops,
      stepsBound := stepsB }
  if s.calibrate then pure { s with stats := s.stats.push st }
  else do
    if Int.ofNat h.allocBytes > heapB then
      die s!"contract heap exceeded: {h.allocBytes} > {heapB}"
    else if Int.ofNat (h.maxDepth * frameBytes) > stackB then
      die s!"contract stack exceeded: {h.maxDepth * frameBytes} > {stackB}"
    else if Int.ofNat h.loops > stepsB then
      die s!"contract steps exceeded: {h.loops} > {stepsB}"
    else pure s

/-- The FNV-1a style digest of the O command. -/
private def caseDigest (s : Session) (lo hi : Int) : SR (Nat × Session) := do
  let mask : Nat := 18446744073709551615
  let mut h : Nat := 0xcbf29ce484222325
  let mut st := s
  let mut r := lo
  while r < hi do
    let (up, st') ← st.call1 st.ids.localeToUpper "localeToUpper"
      [.ptr st.curCell 0 [], .i r]
    let (dn, st') ← st'.call1 st'.ids.localeToLower "localeToLower"
      [.ptr st'.curCell 0 [], .i r]
    st := st'
    h := (h ^^^ toUnsigned .u32 (← asI up)) &&& mask
    h := (h * 0x100000001b3) &&& mask
    h := (h ^^^ toUnsigned .u32 (← asI dn)) &&& mask
    h := (h * 0x100000001b3) &&& mask
    r := r + 1
  pure (h, st)

/-- Evaluate one protocol command, returning its output line. -/
def eval (s : Session) (line : String) : SR (String × Session) := do
  let f := (line.splitOn " ").filter (· ≠ "")
  match f with
  | "P" :: _ => do
    let (posix, s) ← s.call1 s.ids.localePOSIX "LocalePOSIX" []
    pure ("P 1", { s with m := s.m.writeRoot s.curCell posix })
  | ["L", nameT, collT] => do
    let name ← strTok nameT
    let coll ← strTok collT
    let (loc, ok, s) ← s.call2 s.ids.localeSelect "LocaleSelect"
      [.ptr s.baseCell 0 [], .s name, .s coll]
    let okB ← asB ok
    let s := if okB then { s with m := s.m.writeRoot s.curCell loc } else s
    pure (s!"L {if okB then 1 else 0}", s)
  | ["C", flagsT, patT] => do
    let flags ← intTok flagsT
    let pat ← strTok patT
    let cur := s.m.readCell s.curCell
    let (re, err, s) ← s.call2 s.ids.compile "Compile" [.s pat, cur, .i flags]
    let (code, pos) ← errParts s err
    if code != 0 then
      pure (s!"C {code} {pos} 0", { s with valid := false, conCache := [] })
    else do
      let s := { s with m := s.m.writeRoot s.reCell re, valid := true,
                        conCache := [] }
      let (n, s) ← numSub s
      pure (s!"C 0 0 {n}", s)
  | ["X", eflagsT, subjT] => do
    if !s.valid then pure ("X ERR", s)
    else do
      let eflags ← intTok eflagsT
      let subj ← strTok subjT
      let (n, s) ← numSub s
      let (pm, s) ← mkPmatch s (n.toNat + 1)
      let ((_, heapB, stackB, stepsB), s) ← contractOf s subj.size
      let s := { s with m := s.m.resetMeter }
      let (ok, err, s) ← s.call2 s.ids.exec "Exec"
        [.ptr s.reCell 0 [], .s subj, pm, .i eflags]
      let s ← meterCheck s heapB stackB stepsB
      let (code, _) ← errParts s err
      if code != 0 then pure (s!"X {code} 0", s)
      else if !(← asB ok) then pure ("X 0 0", s)
      else do
        let pairs ← pmatchPairs s pm
        let tail := String.join (pairs.map (fun (so, eo) => s!" {so},{eo}"))
        pure (s!"X 0 1{tail}", s)
  | ["R", limitT, eflagsT, replT, subjT] => do
    if !s.valid then pure ("R ERR", s)
    else do
      let limit ← intTok limitT
      let eflags ← intTok eflagsT
      let repl ← strTok replT
      let subj ← strTok subjT
      let (out, err, s) ← s.call2 s.ids.replaceAll "ReplaceAll"
        [.ptr s.reCell 0 [], .s subj, .s repl, .i limit, .i eflags]
      let (code, pos) ← errParts s err
      if code != 0 then pure (s!"R {code} {pos} -", s)
      else pure (s!"R 0 0 {tokEncode (← asS out)}", s)
  | ["I", limitT, eflagsT, subjT] => do
    if !s.valid then pure ("I ERR", s)
    else do
      let limit ← intTok limitT
      let eflags ← intTok eflagsT
      let subj ← strTok subjT
      let (it, err, s) ← s.call2 s.ids.matchIterInit "MatchIterInit"
        [.ptr s.reCell 0 [], .i limit]
      let (code, _) ← errParts s err
      if code != 0 then pure (s!"I {code} 0", s)
      else do
        let (m, itCell) := s.m.alloc it
        let mut s := { s with m }
        let (n, s') ← numSub s
        s := s'
        let (pm, s') ← mkPmatch s (n.toNat + 1)
        s := s'
        let mut rows : Array String := #[]
        let mut failCode : Option Int := none
        let mut going := true
        while going do
          let (ok, nerr, s') ← s.call2 s.ids.matchIterNext "MatchIterNext"
            [.ptr s.reCell 0 [], .ptr itCell 0 [], .s subj, .i eflags, pm]
          s := s'
          let (ncode, _) ← errParts s nerr
          if ncode != 0 then
            failCode := some ncode
            going := false
          else if !(← asB ok) then
            going := false
          else do
            let pairs ← pmatchPairs s pm
            let row := ",".intercalate
              (pairs.map (fun (so, eo) => s!"{so},{eo}"))
            rows := rows.push row
        match failCode with
        | some code => pure (s!"I {code} 0", s)
        | none => do
          let head := s!"I 0 {rows.size}"
          if rows.size == 0 then pure (head, s)
          else pure (head ++ " " ++ "|".intercalate rows.toList, s)
  | ["T", maxT] => do
    if !s.valid then pure ("T ERR", s)
    else do
      let maxInput ← intTok maxT
      let ((hs, heapB, stackB, steps), s) ← contractOf s maxInput
      pure (s!"T {if hs then 1 else 0} {heapB} {stackB} {steps}", s)
  | ["O", loT, hiT] => do
    let lo := IW.i32.wrap (← intTok loT)
    let hi := IW.i32.wrap (← intTok hiT)
    let (h, s) ← caseDigest s lo hi
    pure (s!"O {h}", s)
  | _ => die s!"unknown driver command {line}"

end Session

/--
Parse a corpus dump: one command and its expected output per line, tab separated.
Blank lines are ignored, and malformed nonempty rows fail with their line number.
-/
def parseCorpus (txt : String) : Except String (List (String × String)) := do
  let mut rows : Array (String × String) := #[]
  let mut lineNo := 0
  for line in txt.splitOn "\n" do
    lineNo := lineNo + 1
    if !line.isEmpty then
      match line.splitOn "\t" with
      | [cmd, want] => rows := rows.push (cmd, want)
      | _ => throw s!"malformed corpus row {lineNo}"
  pure rows.toList

structure CorpusResult where
  checked : Nat
  skipped : Nat
  deriving Repr, BEq

/-- One replay step's outcome. -/
inductive StepOut where
  | checked
  | skipped
  | failed (msg : String)

/--
Replay one command under the skip policy.
`reStale` records that the last compile ran out of fuel, so the pattern of the session is behind the reference.
Commands that depend on it are skipped until a compile completes.
A stateful command out of fuel is a hard failure.
A skip there would quietly pull the session out of step with the reference outputs.
Every command that does run must produce the reference output exactly.
-/
def corpusStep (s : Session) (reStale : Bool) (cmd want : String) :
    StepOut × Session × Bool :=
  let kind := cmd.front
  let dependsOnRe :=
    kind == 'X' || kind == 'R' || kind == 'I' || kind == 'T'
  if reStale && dependsOnRe then
    (.skipped, s, reStale)
  else
    match s.eval cmd with
    | .error .outOfFuel =>
      if kind == 'C' then (.skipped, s, true)
      else if dependsOnRe then (.skipped, s, reStale)
      else (.failed "stateful command out of fuel", s, reStale)
    | .error (.fault e) => (.failed e, s, reStale)
    | .ok (got, s') =>
      if got != want then
        (.failed s!"got '{got}' want '{want}'", s, reStale)
      else
        (.checked, s'.compact, if kind == 'C' then false else reStale)

/--
Replay command and expected-output pairs under a fuel bound.
Fuel limits the recursion depth of each engine call.
The bound therefore picks a deterministic subset of the corpus, not a time limit.
-/
def runCorpusFuel (tp : TProgram) (pairs : List (String × String))
    (fuel : Nat := Machine.defaultFuel) : Except String CorpusResult := do
  let render : DriverErr → String := fun e =>
    match e with
    | .outOfFuel => "out of fuel"
    | .fault msg => msg
  let s0 ← (Session.start tp).mapError render
  let mut s := { s0 with fuel := fuel }
  let mut checked := 0
  let mut skipped := 0
  let mut reStale := false
  let mut idx := 0
  for (cmd, want) in pairs do
    match corpusStep s reStale cmd want with
    | (.checked, s', st') =>
      s := s'
      reStale := st'
      checked := checked + 1
    | (.skipped, s', st') =>
      s := s'
      reStale := st'
      skipped := skipped + 1
    | (.failed msg, _, _) => throw s!"command {idx} '{cmd}': {msg}"
    idx := idx + 1
  pure { checked, skipped }

/-- The checked revera program. -/
def reveraChecked : Except String TProgram :=
  match decodeProgram reveraJsonText with
  | .ok p => elabProgram p
  | .error e => .error e

end Vego
