/-
Command-line checker: replays a corpus of driver commands with
expected outputs (the tab-separated dump of crosscheck) against
the interpreted revera engine, and reports progress and the first
divergence. The theorems cover the embedded corpus under a fuel
budget; this executable scales the same check to any dump and any
budget.

With --contracts the replay measures every Exec against the
pattern's resource contract instead of failing on the first
excess, and reports the tightest margins as it goes. This is the
calibration mode: it shows how close the real executions come to
the contract figures. A few corpus patterns run for hours in the
interpreter, so the margins print during the replay and not only
at the end.

Usage: vegocheck [--contracts] [corpus.tsv] [fuel] [limit]
-/

import Vego.Driver

open Vego

/-- used/bound in permille, for integer-only reporting. A
non-positive bound with any use is reported as far over. -/
def permille (used : Nat) (bound : Int) : Nat :=
  if bound ≤ 0 then (if used == 0 then 0 else 1000000)
  else (used * 1000) / bound.toNat

/-- The worst margin seen for one meter, with the command that
produced it. -/
structure Worst where
  ratio : Nat := 0
  over : Nat := 0
  seen : Bool := false
  cmd : String := ""
  st : MeterStat := default

def Worst.add (w : Worst) (cmd : String) (st : MeterStat)
    (r : Nat) : Worst :=
  { ratio := if r > w.ratio then r else w.ratio,
    over := if r > 1000 then w.over + 1 else w.over,
    seen := true,
    cmd := if r > w.ratio || !w.seen then cmd else w.cmd,
    st := if r > w.ratio || !w.seen then st else w.st }

def statLine (cmd : String) (st : MeterStat) : String :=
  s!"heap {st.heapUsed}/{st.heapBound} " ++
  s!"stack {st.depthUsed * frameBytes}/{st.stackBound} " ++
  s!"steps {st.stepsUsed}/{st.stepsBound} " ++
  s!"loops {st.loopsUsed}/{st.stepsBound} : {cmd.take 60}"

/-- The four meters, each with the ratio it reports. -/
def meterRatio : List (String × (MeterStat → Nat)) :=
  [("heap", fun st => permille st.heapUsed st.heapBound),
   ("stack", fun st => permille (st.depthUsed * frameBytes) st.stackBound),
   ("steps", fun st => permille st.stepsUsed st.stepsBound),
   ("loops", fun st => permille st.loopsUsed st.stepsBound)]

def reportWorsts (measured : Nat) (ws : Array Worst) : IO Unit := do
  IO.println s!"measured Exec calls: {measured}"
  for (name, _) in meterRatio, w in ws do
    if w.seen then
      IO.println s!"{name}: worst {w.ratio} permille, {w.over} over"
      IO.println s!"  {statLine w.cmd w.st}"
    else
      IO.println s!"{name}: no data"

def main (args : List String) : IO UInt32 := do
  let contracts := args.contains "--contracts"
  let args := args.filter (· != "--contracts")
  let path := args.headD "corpus.tsv"
  let fuel := ((args[1]?).bind (·.toNat?)).getD Machine.defaultFuel
  let limit := ((args[2]?).bind (·.toNat?)).getD 1000000000
  let txt ← IO.FS.readFile path
  let pairs := (parseCorpus txt).take limit
  IO.println s!"corpus: {pairs.length} commands, fuel {fuel}"
  match reveraChecked with
  | .error e =>
    IO.println s!"revera does not check: {e}"
    return 1
  | .ok tp =>
    match Session.start tp with
    | .error _ =>
      IO.println "session start failed"
      return 1
    | .ok s0 => do
      let stdout ← IO.getStdout
      let t0 ← IO.monoMsNow
      let mut s := { s0 with fuel,
                             enforce := !contracts,
                             collect := contracts }
      let mut checked := 0
      let mut skipped := 0
      let mut reStale := false
      let mut idx := 0
      let mut measured := 0
      let mut worsts : Array Worst :=
        Array.replicate meterRatio.length {}
      for (cmd, want) in pairs do
        let tc ← IO.monoMsNow
        match corpusStep s reStale cmd want with
        | (.checked, s', st') =>
          s := s'
          reStale := st'
          checked := checked + 1
        | (.skipped, s', st') =>
          s := s'
          reStale := st'
          skipped := skipped + 1
        | (.failed msg, _, _) =>
          IO.println s!"FAIL at {idx} '{cmd}': {msg}"
          return 1
        -- Fold every measurement this command produced into the
        -- running worsts, then drop it: a long replay must not
        -- accumulate one record per Exec.
        for st in s.stats do
          measured := measured + 1
          let mut i := 0
          for (_, ratio) in meterRatio do
            worsts := worsts.modify i (fun w => w.add cmd st (ratio st))
            i := i + 1
        if !s.stats.isEmpty then
          s := { s with stats := #[] }
        let dtc := (← IO.monoMsNow) - tc
        if dtc > 3000 then
          IO.println s!"slow command {idx} ({dtc} ms): {cmd.take 60}"
          stdout.flush
        idx := idx + 1
        if idx % 2000 == 0 then
          let dt := (← IO.monoMsNow) - t0
          IO.println s!"{idx} commands, {dt} ms"
          if contracts then reportWorsts measured worsts
          stdout.flush
      let dt := (← IO.monoMsNow) - t0
      IO.println
        s!"done: {checked} agree, {skipped} skipped by fuel ({dt} ms)"
      if contracts then reportWorsts measured worsts
      return 0
