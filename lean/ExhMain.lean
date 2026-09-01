/-
exhaustcheck: run the exhaustive sweeps of `Vego.Exhaustive` and report their coverage.
This is the diagnostic form of `Vego.exhaustiveAgrees`: it names the first disagreement.
-/

import Vego.Exhaustive

open Vego

def runSweep (tp : TProgram) (name : String) (sw : Sweep) (limit : Option Nat) : IO Bool := do
  let sw := match limit with
    | some n => { sw with patterns := sw.patterns.take n }
    | none => sw
  let t0 ← IO.monoMsNow
  IO.println s!"{name}: {sw.patterns.length} patterns, {sw.subjects.length} subjects, {sw.cflags.length} cflags, {sw.eflags.length} eflags"
  match exhaustiveRun tp sw specBudget with
  | .ok cov =>
    IO.println s!"  coverage: {repr cov}"
    IO.println s!"  {(← IO.monoMsNow) - t0}ms"
    pure true
  | .error e =>
    IO.println s!"  DISAGREEMENT: {e}"
    pure false

def main (args : List String) : IO UInt32 := do
  let limit := (args[0]?).bind String.toNat?
  match reveraChecked with
  | .error e => IO.eprintln e; pure 1
  | .ok tp =>
    let a ← runSweep tp "structure" sweepStructure limit
    let b ← runSweep tp "flags" sweepFlags limit
    pure (if a && b then 0 else 2)
