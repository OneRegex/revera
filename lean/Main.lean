/-
Command-line checker.
It replays a corpus of driver commands with expected outputs against the interpreted revera engine.
That corpus is the tab-separated dump of crosscheck.
It reports progress and the first divergence.
The theorems cover the embedded corpus under a fuel budget.
This executable scales the same check to any dump and any budget.

With --contracts the replay measures every Exec against the resource contract of its pattern.
It does not fail on the first excess.
It reports the tightest margins as it goes.
This is the calibration mode.
It shows how close the real executions come to the contract figures.
A few corpus patterns run for hours in the interpreter.
The margins therefore print during the replay, and not only at the end.

Usage: vegocheck [--contracts] [corpus.tsv] [fuel] [limit]
-/

import Vego.Driver

open Vego

/--
used/bound in permille, for integer-only reporting.
A non-positive bound with any use is reported as far over.
-/
def permille (used : Nat) (bound : Int) : Nat :=
  if bound ≤ 0 then (if used == 0 then 0 else 1000000)
  else (used * 1000) / bound.toNat

/-- The worst margin seen for one meter, with the command that produced it. -/
structure Worst where
  ratio : Nat := 0
  over : Nat := 0
  win : Option (String × MeterStat) := none

def Worst.add (w : Worst) (cmd : String) (st : MeterStat)
    (r : Nat) : Worst :=
  let over := if r > 1000 then w.over + 1 else w.over
  if w.win.isNone || r > w.ratio then
    { ratio := r, over, win := some (cmd, st) }
  else { w with over }

def statLine (cmd : String) (st : MeterStat) : String :=
  s!"heap {st.heapUsed}/{st.heapBound} " ++
  s!"stack {st.depthUsed * frameBytes}/{st.stackBound} " ++
  s!"steps {st.stepsUsed}/{st.stepsBound} " ++
  s!"loops {st.loopsUsed}/{st.stepsBound} : {cmd.take 60}"

/--
The meters, each with the ratio it reports and whether the session enforces it.
The step counter is reported, but not enforced.
It counts one unit per executed statement, which is finer than the abstract operations of the contract.
The fixed setup of Exec therefore takes it over the figure on tiny subjects.
The loop counter is the enforced one.
-/
def meterRatio : List (String × Bool × (MeterStat → Nat)) :=
  [("heap", true, fun st => permille st.heapUsed st.heapBound),
   ("stack", true,
    fun st => permille (st.depthUsed * frameBytes) st.stackBound),
   ("steps", false, fun st => permille st.stepsUsed st.stepsBound),
   ("loops", true, fun st => permille st.loopsUsed st.stepsBound)]

def reportWorsts (measured : Nat) (ws : List Worst) : IO Unit := do
  IO.println s!"measured Exec calls: {measured}"
  for (name, enforced, _) in meterRatio, w in ws do
    let tag := if enforced then "" else " (reported only)"
    match w.win with
    | some (cmd, st) =>
      IO.println s!"{name}{tag}: worst {w.ratio} permille, {w.over} over"
      IO.println s!"  {statLine cmd st}"
    | none => IO.println s!"{name}{tag}: no data"

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
      let mut s := { s0 with fuel, calibrate := contracts }
      let mut checked := 0
      let mut skipped := 0
      let mut reStale := false
      let mut idx := 0
      let mut measured := 0
      let mut worsts : List Worst := meterRatio.map (fun _ => {})
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
        -- Fold every measurement this command produced into the running worsts, then drop it.
        -- A long replay must not accumulate one record per Exec.
        for st in s.stats do
          measured := measured + 1
          worsts := (meterRatio.zip worsts).map
            (fun ((_, _, ratio), w) => w.add cmd st (ratio st))
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
