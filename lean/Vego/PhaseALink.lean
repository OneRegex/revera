/-
The phase A model against the interpreted engine, on the corpus.

For every execution the driver session runs on a pattern that needs no capture offsets, the compiled program
and the bracket tables are read out of the interpreted `Regexp`, the model runs on the same subject, and
three things are compared with the interpreter: the match result, the bytes allocated, and the loop meter
against the model's step figure.
Such executions run phase A and nothing else, so the interpreter's figures are phase A's figures plus the
fixed work of `Exec` around it.

The link covers the POSIX locale, where a bracket consumes one character, and programs that were not
pruned by the size cap. The rest is counted.
-/

import Vego.PhaseA
import Vego.CorpusData
import Ere.Locale

namespace Vego
namespace PhaseA

/-- The struct field positions the extraction reads, resolved once by name. -/
structure Layout where
  reBrackets : Nat
  reMulti : Nat
  reFlags : Nat
  reMinSlots : Nat
  reNsub : Nat
  reProgOK : Nat
  reProg : Nat
  progIns : Nat
  progStart : Nat
  progFoldSets : Nat
  progFailMin : Nat
  progScan : Nat
  progDepth : Nat
  insOp : Nat
  insNext : Nat
  insAlt : Nat
  insArg : Nat
  insMask : Nat
  insExtra : Nat
  scanEnabled : Nat
  scanSingle : Nat
  scanB : Nat
  scanStop : Nat
  brNegated : Nat
  brIcase : Nat
  brNlMode : Nat
  brRanges : Nat
  brClassMask : Nat
  brEquivs : Nat
  brMultiLens : Nat
  rrLo : Nat
  rrHi : Nat
  atomCost : Nat

def resolveLayout (m : Machine) : Except String Layout := do
  let fld (st f : String) : Except String Nat := do
    let some si := m.structIdx? st | throw s!"no struct {st}"
    let some fields := m.prog.structFields[si]? | throw "bad struct index"
    let some i := fields.idxOf? f | throw s!"no field {st}.{f}"
    pure i
  let fn (n : String) : Except String Nat :=
    match m.fnIdx? n with
    | some i => pure i
    | none => throw s!"no function {n}"
  pure { reBrackets := ← fld "Regexp" "brackets", reMulti := ← fld "Regexp" "multi",
         reFlags := ← fld "Regexp" "flags",
         reMinSlots := ← fld "Regexp" "minSlots", reNsub := ← fld "Regexp" "nsub",
         reProgOK := ← fld "Regexp" "progOK", reProg := ← fld "Regexp" "prog",
         progIns := ← fld "program" "ins", progStart := ← fld "program" "start",
         progFoldSets := ← fld "program" "foldSets", progFailMin := ← fld "program" "failMin",
         progScan := ← fld "program" "scan", progDepth := ← fld "program" "depth",
         insOp := ← fld "instr" "op", insNext := ← fld "instr" "next", insAlt := ← fld "instr" "alt",
         insArg := ← fld "instr" "arg", insMask := ← fld "instr" "mask", insExtra := ← fld "instr" "extra",
         scanEnabled := ← fld "scanFilter" "enabled", scanSingle := ← fld "scanFilter" "single",
         scanB := ← fld "scanFilter" "b", scanStop := ← fld "scanFilter" "stop",
         brNegated := ← fld "bracketSet" "negated", brIcase := ← fld "bracketSet" "icase",
         brNlMode := ← fld "bracketSet" "nlMode", brRanges := ← fld "bracketSet" "ranges",
         brClassMask := ← fld "bracketSet" "classMask", brEquivs := ← fld "bracketSet" "equivs",
         brMultiLens := ← fld "bracketSet" "multiLens",
         rrLo := ← fld "runeRange" "lo", rrHi := ← fld "runeRange" "hi",
         atomCost := ← fn "atomCost" }

/-- A compiled bracket, as the model tests it in the POSIX locale. -/
structure BracketData where
  negated : Bool
  icase : Bool
  nlMode : Bool
  ranges : List (Int × Int)
  classMask : Nat
  equivs : List (List Int)

/-- Everything the model needs from one compiled `Regexp`. -/
structure Extracted where
  prog : Prog
  foldSets : Array (List Int)
  brackets : Array BracketData
  nosub : Bool
  newline : Bool
  nsub : Nat
  /-- `program.depth`: the bound of the anchored figure, or -1 when the program has none. -/
  depth : Int

private def asI (v : Val) : Except String Int :=
  match v with
  | .i n => pure n
  | _ => throw "expected an integer"

private def asB (v : Val) : Except String Bool :=
  match v with
  | .b x => pure x
  | _ => throw "expected a bool"

private def field (v : Val) (i : Nat) : Except String Val :=
  match v with
  | .strukt fs => match fs[i]? with
    | some x => pure x
    | none => throw "bad field index"
  | _ => throw "expected a struct"

private def elems (m : Machine) (v : Val) : Except String (Array Val) :=
  match m.sliceElems v with
  | some es => pure es
  | none => throw "unreadable slice"

private def intList (m : Machine) (v : Val) : Except String (List Int) := do
  let es ← elems m v
  es.toList.mapM asI

/-- The set bits of a 64-bit mask, ascending. -/
private def maskBits (mask : Nat) : List Nat :=
  (List.range 64).filter fun i => (mask >>> i) % 2 == 1

private def opOf (n : Int) : Except String Op :=
  match n with
  | 0 => pure .rune | 1 => pure .runeFold | 2 => pure .any | 3 => pure .bracket | 4 => pure .bol
  | 5 => pure .eol | 6 => pure .split | 7 => pure .jmp | 8 => pure .accept | 9 => pure .fail
  | _ => throw "bad opcode"

/-- The `failMin` sentinel of a program with no pruned subtree. -/
def failMinNone : Int := 4611686018427387904

/-- Why a pattern is outside the link. -/
inductive Outside where
  | captures
  | pruned
  | multi

/-- Read the program of the current pattern, or say why the link leaves it out. -/
def extract (m : Machine) (L : Layout) (reCell : Nat) : Except String (Outside ⊕ Extracted) := do
  let re := m.readCell reCell
  let flags ← asI (← field re L.reFlags)
  let nsub ← asI (← field re L.reNsub)
  let progOK ← asB (← field re L.reProgOK)
  let prog ← field re L.reProg
  let failMin ← asI (← field prog L.progFailMin)
  let depth ← asI (← field prog L.progDepth)
  let nosub := (flags / 4) % 2 == 1
  if !progOK || failMin != failMinNone then return (.inl .pruned)
  if !nosub && nsub != 0 then return (.inl .captures)
  let k := (← asI (← field re L.reMinSlots)).toNat
  let multi ← asB (← field re L.reMulti)
  let ring := if multi then maxElemAhead + 1 else 2
  let insVals ← elems m (← field prog L.progIns)
  let mut ins : Array Instr := #[]
  for v in insVals do
    let op ← opOf (← asI (← field v L.insOp))
    let next := (← asI (← field v L.insNext)).toNat
    let alt := (← asI (← field v L.insAlt)).toNat
    let arg := (← asI (← field v L.insArg)).toNat
    let mask := (← asI (← field v L.insMask)).toNat
    let extra ← intList m (← field v L.insExtra)
    ins := ins.push { op, next, alt, arg, slots := maskBits mask ++ extra.map Int.toNat }
  let start := (← asI (← field prog L.progStart)).toNat
  let scanV ← field prog L.progScan
  let enabled ← asB (← field scanV L.scanEnabled)
  let single ← asB (← field scanV L.scanSingle)
  let b := (← asI (← field scanV L.scanB)).toNat
  let stopV ← field scanV L.scanStop
  let stop ← match stopV with
    | .arr es => es.toList.mapM asB
    | _ => throw "expected the stop array"
  let stopArr := stop.toArray
  let scan : Scan := { enabled, single, b := UInt8.ofNat b,
                       stop := fun byte => stopArr.getD byte.toNat false }
  let foldVals ← elems m (← field prog L.progFoldSets)
  let foldSets ← foldVals.mapM (intList m)
  let brVals ← elems m (← field re L.reBrackets)
  let mut brackets : Array BracketData := #[]
  for v in brVals do
    let multiLens ← asI (← field v L.brMultiLens)
    if multiLens != 0 then return (.inl .multi)
    let rangeVals ← elems m (← field v L.brRanges)
    let ranges ← rangeVals.toList.mapM fun rv => do
      pure ((← asI (← field rv L.rrLo)), (← asI (← field rv L.rrHi)))
    let equivVals ← elems m (← field v L.brEquivs)
    let equivs ← equivVals.toList.mapM (intList m)
    brackets := brackets.push
      { negated := ← asB (← field v L.brNegated), icase := ← asB (← field v L.brIcase),
        nlMode := ← asB (← field v L.brNlMode), ranges,
        classMask := (← asI (← field v L.brClassMask)).toNat, equivs }
  pure (.inr { prog := { ins, start, k, ring, scan }, foldSets, brackets, nosub,
               newline := (flags / 2) % 2 == 1, nsub := nsub.toNat, depth })

/-! ## The atom tests of the POSIX locale, from `bracket.go` and `locale.go` -/

/-- `posixMask`: the class bits of one character, in the class order of `classNames`. -/
def posixMask (r : Int) : Nat :=
  if r < 0 || r > 0x10FFFF || (0xD800 ≤ r && r ≤ 0xDFFF) then 0
  else
    let c := r.toNat
    let bits : List Bool :=
      [Ere.Posix.alnum c, Ere.Posix.alpha c, Ere.Posix.blank c, Ere.Posix.cntrl c, Ere.Posix.digit c,
       Ere.Posix.graph c, Ere.Posix.lower c, Ere.Posix.print c, Ere.Posix.punct c, Ere.Posix.space c,
       Ere.Posix.upper c, Ere.Posix.xdigit c]
    (bits.zipIdx.map fun (b, i) => if b then 1 <<< i else 0).sum

def positiveSingle (b : BracketData) (c : Int) : Bool :=
  b.ranges.any (fun (lo, hi) => lo ≤ c && c ≤ hi) ||
  (b.classMask != 0 && (posixMask c &&& b.classMask) != 0) ||
  b.equivs.any (fun seq => seq == [c])

/-- `bracketMatchesOne` in the POSIX locale: the case preimages are the ASCII counterparts. -/
def bracketMatchesOne (b : BracketData) (c : Int) : Bool :=
  if c < 0 then false
  else if b.negated && b.nlMode && c == 10 then false
  else
    let want := !b.negated
    if positiveSingle b c == want then true
    else if !b.icase then false
    else
      let pre : List Int :=
        if 65 ≤ c && c ≤ 90 then [c + 32] else if 97 ≤ c && c ≤ 122 then [c - 32] else []
      pre.any fun p => positiveSingle b p == want

def anyMatches (nlMode : Bool) (c : Int) : Bool :=
  if c ≤ 0 then false else !(nlMode && c == 10)

def atomsOf (x : Extracted) (nlMode : Bool) : Atoms :=
  { single := fun pc cur =>
      match x.prog.ins[pc]? with
      | some ins =>
        match ins.op with
        | .rune => cur == (ins.arg : Int)
        | .runeFold => (x.foldSets.getD ins.arg []).contains cur
        | .any => anyMatches nlMode cur
        | .bracket => match x.brackets[ins.arg]? with
          | some b => bracketMatchesOne b cur
          | none => false
        | _ => false
      | none => false
    lens := fun _ => []
    multi := fun _ _ => false }

/-! ## The contract of phase A: the figures `PhaseAProofs.lean` proves, plus the stack figure -/

def matcherStackBytes : Nat := 2048
def frameBytes' : Nat := 256
/-- The frames below a multi-character probe, as `go/contract.go` counts them. -/
def multiLookupFrames : Nat := maxElemAhead + 8
/-- The matcher stack figure of `go/contract.go`, which a NoSub pattern reports for the whole call. -/
def matcherStackFigure : Nat := matcherStackBytes + multiLookupFrames * frameBytes'

/-! ## The certificate of a bounded program, behind the anchored figure -/

/-- The successors of one instruction, as `progEdge` of `program.go` lists them. -/
def edgesOf (p : Prog) (pc : Nat) : List Nat :=
  let ins := p.ins.getD pc default
  match ins.op with
  | .accept | .fail => []
  | .split => [ins.next, ins.alt]
  | _ => [ins.next]

/-- One relaxation round of the depth labels: every edge raises its target to its source plus its weight. -/
def relaxRound (p : Prog) (d : Array Nat) : Array Nat × Bool := Id.run do
  let mut d := d
  let mut changed := false
  for pc in List.range p.n do
    let w := if (p.ins.getD pc default).op.consuming then 1 else 0
    for q in edgesOf p pc do
      if d.getD q 0 < d.getD pc 0 + w then
        d := d.setIfInBounds q (d.getD pc 0 + w)
        changed := true
  return (d, changed)

/--
The depth labels by relaxation to a fixed point: the longest consuming path into every instruction.
A consuming cycle keeps the labels growing, and the relaxation then gives up after `n + 1` rounds.
-/
def depthLabels (p : Prog) : Option (Array Nat) := Id.run do
  let mut d := Array.replicate p.n 0
  for _ in List.range (p.n + 1) do
    let (d', changed) := relaxRound p d
    if !changed then return some d
    d := d'
  return none

/-- One closure round of the seed reach: every marked split or jump marks its targets. -/
def reachRound (p : Prog) (seen : Array Bool) : Array Bool × Bool := Id.run do
  let mut seen := seen
  let mut changed := false
  for pc in List.range p.n do
    if seen.getD pc false then
      let ins := p.ins.getD pc default
      let targets := match ins.op with
        | .split => [ins.next, ins.alt]
        | .jmp => [ins.next]
        | _ => []
      for q in targets do
        if !seen.getD q false then
          seen := seen.setIfInBounds q true
          changed := true
  return (seen, changed)

/-- The instructions the spawn of a mid-subject boundary reaches: the start, closed under the split and jump edges. -/
def seedReach (p : Prog) : Array Bool := Id.run do
  let mut seen := (Array.replicate p.n false).setIfInBounds p.start true
  for _ in List.range (p.n + 1) do
    let (seen', changed) := reachRound p seen
    if !changed then return seen
    seen := seen'
  return seen

def maxLabel (d : Array Nat) : Nat := d.foldl max 0

/-- The step figure of a program of the given depth: the anchored figure when the depth is bounded. -/
def stepsFigureOf (p : Prog) (atom len : Nat) (depth : Int) : Nat :=
  if depth < 0 then stepsFigure p atom len else stepsFigureAnchored p atom len depth.toNat

/--
The step figure the contract reports for a program of the given depth, with the certificate of the anchored
figure checked first.
A bounded depth needs the newline flag off, the relaxation labels must settle, `anchoredCheck` must accept
them with the seed reach at the depth the engine computed, and that depth must be the largest label.
The figure is then `stepsFigureAnchored` under the hypotheses of the proof, and the engine's depth is exact.
-/
def contractStepFigure (p : Prog) (atom len : Nat) (depth : Int) (newline : Bool) : Except String Nat :=
  if depth < 0 then pure (stepsFigure p atom len)
  else if newline then throw s!"engine depth {depth} under newline mode"
  else
    match depthLabels p with
    | none => throw s!"engine depth {depth} on a program whose labels do not settle"
    | some d =>
      if !anchoredCheck p d (seedReach p) depth.toNat then throw s!"engine depth {depth} fails the certificate"
      else if maxLabel d != depth.toNat then throw s!"engine depth {depth}, labels reach {maxLabel d}"
      else pure (stepsFigureOf p atom len depth)

/-! ## The walk -/

structure LinkCoverage where
  execsChecked : Nat := 0
  execsCaptures : Nat := 0
  execsNonPosix : Nat := 0
  execsPruned : Nat := 0
  contractsChecked : Nat := 0
  contractsAnchored : Nat := 0
  programsWf : Nat := 0
  deriving Repr, BEq, DecidableEq, Inhabited

/-- What a run of the link reports: the coverage, and the first disagreement if any. -/
structure LinkReport where
  cov : LinkCoverage := {}
  failure : Option String := none
  worstHeapRatio : Nat := 0
  worstStepRatio : Nat := 0
  deriving Repr

/-- The clamp `ContractFor` applies to its argument: nonnegative, and at most the subject limit. -/
def clampInput (maxInput : Int) : Nat :=
  Nat.min (Int.toNat (max maxInput 0)) 2147483647

private def parseT (line : String) : Option (Nat × Nat × Nat × Nat) :=
  match (line.splitOn " ").filter (· ≠ "") with
  | ["T", hs, heap, stack, steps] =>
    match hs.toNat?, heap.toNat?, stack.toNat?, steps.toNat? with
    | some a, some b, some c, some d => some (a, b, c, d)
    | _, _, _, _ => none
  | _ => none

private def parseX (line : String) : Option (Bool × Nat × Nat) :=
  match (line.splitOn " ").filter (· ≠ "") with
  | ["X", "0", "0"] => some (false, 0, 0)
  | "X" :: "0" :: "1" :: first :: _ =>
    match first.splitOn "," with
    | [so, eo] => match so.toNat?, eo.toNat? with
      | some so, some eo => some (true, so, eo)
      | _, _ => none
    | _ => none
  | _ => none

/--
Walk the corpus: the interpreted session answers every command, and on each covered execution the model is
run on the extracted program and compared with the interpreter.
-/
def walk (tp : TProgram) (pairs : List (String × String)) : LinkReport := Id.run do
  let render : DriverErr → String := fun e =>
    match e with
    | .outOfFuel => "out of fuel"
    | .fault msg => msg
  let mut rep : LinkReport := {}
  let s0 ← match Session.start tp with
    | .ok s => pure s
    | .error e => return { rep with failure := some (render e) }
  let L ← match resolveLayout s0.m with
    | .ok L => pure L
    | .error e => return { rep with failure := some e }
  let mut s := s0
  let mut posix := true
  let mut cur : Option Extracted := none
  let mut nonPosixPattern := false
  let mut prunedPattern := false
  let mut multiPattern := false
  let mut atom : Nat := 0
  let mut idx := 0
  for (cmd, want) in pairs do
    let kind := cmd.front
    match s.eval cmd with
    | .error e => return { rep with failure := some s!"command {idx} '{cmd}': {render e}" }
    | .ok (got, s') =>
      s := s'.compact
      if got != want then
        return { rep with failure := some s!"command {idx} '{cmd}': engine '{got}' want '{want}'" }
      if kind == 'P' then posix := true
      else if kind == 'L' then posix := false
      else if kind == 'C' then
        cur := none
        nonPosixPattern := !posix
        prunedPattern := false
        multiPattern := false
        if s.valid && posix then
          match extract s.m L s.reCell with
          | .error e => return { rep with failure := some s!"command {idx} '{cmd}': extract: {e}" }
          | .ok (.inl .pruned) => prunedPattern := true
          | .ok (.inl .multi) => multiPattern := true
          | .ok (.inl .captures) => pure ()
          | .ok (.inr x) =>
            if !x.prog.wfCheck then
              return { rep with failure := some s!"command {idx} '{cmd}': program fails wfCheck" }
            rep := { rep with cov := { rep.cov with programsWf := rep.cov.programsWf + 1 } }
            match s.m.call "atomCost" [.ptr s.reCell 0 []] with
            | .ok ([.i a], m') =>
              s := { s with m := m' }
              atom := a.toNat
              cur := some x
            | _ => return { rep with failure := some s!"command {idx}: atomCost" }
      else if kind == 'T' then
        match cur with
        | none => pure ()
        | some x =>
          if x.nosub then
            match (cmd.splitOn " ").filter (· ≠ ""), parseT got with
            | ["T", maxT], some (hs, heap, stack, steps) =>
              let some maxInput := maxT.toInt? | return { rep with failure := some "bad T argument" }
              let len := clampInput maxInput
              if hs != 0 then return { rep with failure := some s!"command {idx} '{cmd}': solver on a NoSub pattern" }
              if heap != heapFigure x.prog.n x.prog.k x.prog.ring then
                return { rep with failure := some s!"command {idx} '{cmd}': engine heap figure {heap}, proven {heapFigure x.prog.n x.prog.k x.prog.ring}" }
              let fig ← match contractStepFigure x.prog atom len x.depth x.newline with
                | .ok f => pure f
                | .error e => return { rep with failure := some s!"command {idx} '{cmd}': {e}" }
              if steps != fig then
                return { rep with failure := some s!"command {idx} '{cmd}': engine step figure {steps}, proven {fig}" }
              if stack != matcherStackFigure then
                return { rep with failure := some s!"command {idx} '{cmd}': engine stack figure {stack}, expected {matcherStackFigure}" }
              let anchored := if x.depth < 0 then 0 else 1
              let cov := rep.cov
              rep := { rep with cov := { cov with contractsChecked := cov.contractsChecked + 1, contractsAnchored := cov.contractsAnchored + anchored } }
            | _, _ => return { rep with failure := some s!"command {idx}: unparsable T" }
      else if kind == 'X' then
        match cur with
        | none =>
          if nonPosixPattern then rep := { rep with cov := { rep.cov with execsNonPosix := rep.cov.execsNonPosix + 1 } }
          else if prunedPattern || multiPattern then rep := { rep with cov := { rep.cov with execsPruned := rep.cov.execsPruned + 1 } }
          else rep := { rep with cov := { rep.cov with execsCaptures := rep.cov.execsCaptures + 1 } }
        | some x =>
          match (cmd.splitOn " ").filter (· ≠ ""), parseX got with
          | ["X", eflagsT, subjT], some (matched, so, eo) =>
            let some eflags := eflagsT.toNat? | return { rep with failure := some "bad eflags" }
            let some subj := tokDecode subjT | return { rep with failure := some "bad subject" }
            let flagsNl := x.newline
            let input : Input := { bytes := subj, notbol := eflags % 2 == 1, noteol := (eflags / 2) % 2 == 1,
                                   nlMode := flagsNl }
            let r := run x.prog (atomsOf x flagsNl) input
            -- Under NoSub the engine leaves pmatch zeroed, so only the matched flag is comparable.
            if r.matched != matched || (matched && !x.nosub && (r.so != so || r.eo != eo)) then
              return { rep with failure := some s!"command {idx} '{cmd}': model {r.matched} {r.so} {r.eo}, engine {got}" }
            let heapInterp := s.m.heap.allocBytes
            let loopsInterp := s.m.heap.loops
            if r.m.allocBytes != heapInterp then
              return { rep with failure := some s!"command {idx} '{cmd}': model heap {r.m.allocBytes}, engine {heapInterp}" }
            let fig := stepFigure r.m x.prog.k x.prog.ring atom
            if loopsInterp > fig then
              return { rep with failure := some s!"command {idx} '{cmd}': engine loops {loopsInterp} over model figure {fig}: {repr r.m}" }
            let bound := stepsFigureOf x.prog atom subj.size x.depth
            let hbound := heapFigure x.prog.n x.prog.k x.prog.ring
            rep := { rep with cov := { rep.cov with execsChecked := rep.cov.execsChecked + 1 },
                              worstStepRatio := Nat.max rep.worstStepRatio (fig * 1000 / Nat.max bound 1),
                              worstHeapRatio := Nat.max rep.worstHeapRatio (r.m.allocBytes * 1000 / Nat.max hbound 1) }
          | _, _ => return { rep with failure := some s!"command {idx}: unparsable X" }
      idx := idx + 1
  pure rep

/-- The coverage the theorem pins down. -/
def linkCoverage : LinkCoverage :=
  { execsChecked := 49682, execsCaptures := 26854, execsNonPosix := 1700, execsPruned := 0, contractsChecked := 56,
    contractsAnchored := 10, programsWf := 6913 }

/--
The interpreted engine agrees with the phase A model on every covered corpus execution, its heap bytes are
exactly the model's, its loop count never exceeds the model's step figure, and the contract figures the
engine reports for NoSub patterns are the figures `PhaseAProofs.lean` proves. Every extracted program passes
`Prog.wfCheck`, the decidable form of the program hypotheses of the universal theorems.
-/
def linkAgrees (_ : Unit) : Bool :=
  match corpusPairs, reveraChecked with
  | .ok pairs, .ok tp =>
    let rep := walk tp (sensiblePairs pairs)
    rep.failure.isNone && rep.cov == linkCoverage
  | _, _ => false

end PhaseA
end Vego
