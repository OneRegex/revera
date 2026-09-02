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
         progScan := ← fld "program" "scan",
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
               newline := (flags / 2) % 2 == 1, nsub := nsub.toNat })

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
def equivFrames : Nat := maxElemAhead + 2

/-! ## The walk -/

structure LinkCoverage where
  execsChecked : Nat := 0
  execsCaptures : Nat := 0
  execsNonPosix : Nat := 0
  execsPruned : Nat := 0
  contractsChecked : Nat := 0
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
            | ["T", maxT], some (hs, heap, _, steps) =>
              let some maxInput := maxT.toInt? | return { rep with failure := some "bad T argument" }
              let len := clampInput maxInput
              if hs != 0 then return { rep with failure := some s!"command {idx} '{cmd}': solver on a NoSub pattern" }
              if heap != heapFigure x.prog.n x.prog.k x.prog.ring then
                return { rep with failure := some s!"command {idx} '{cmd}': engine heap figure {heap}, proven {heapFigure x.prog.n x.prog.k x.prog.ring}" }
              if steps != stepsFigure x.prog atom len then
                return { rep with failure := some s!"command {idx} '{cmd}': engine step figure {steps}, proven {stepsFigure x.prog atom len}" }
              rep := { rep with cov := { rep.cov with contractsChecked := rep.cov.contractsChecked + 1 } }
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
            let bound := stepsFigure x.prog atom subj.size
            let hbound := heapFigure x.prog.n x.prog.k x.prog.ring
            rep := { rep with cov := { rep.cov with execsChecked := rep.cov.execsChecked + 1 },
                              worstStepRatio := Nat.max rep.worstStepRatio (fig * 1000 / Nat.max bound 1),
                              worstHeapRatio := Nat.max rep.worstHeapRatio (r.m.allocBytes * 1000 / Nat.max hbound 1) }
          | _, _ => return { rep with failure := some s!"command {idx}: unparsable X" }
      idx := idx + 1
  pure rep

/-- The coverage the theorem pins down. -/
def linkCoverage : LinkCoverage :=
  { execsChecked := 47922, execsCaptures := 25974, execsNonPosix := 1160, execsPruned := 0, contractsChecked := 46,
    programsWf := 6893 }

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
