/-
phasealink: the phase A model against the interpreted engine on a corpus dump.
This is the diagnostic form of `Vego.PhaseA.linkAgrees`: it reports coverage, the worst ratios of the
model's figures to the contract figures, and the first disagreement.
-/

import Vego.PhaseALink

open Vego Vego.PhaseA

def main (args : List String) : IO UInt32 := do
  let path := args.headD "data/corpus.tsv"
  let limit := (args[1]?).bind String.toNat?
  let txt ← IO.FS.readFile path
  match parseCorpus txt with
  | .error e => IO.eprintln e; pure 1
  | .ok pairs =>
    let pairs := sensiblePairs pairs
    let pairs := match limit with | some n => pairs.take n | none => pairs
    match reveraChecked with
    | .error e => IO.eprintln e; pure 1
    | .ok tp =>
      let t0 ← IO.monoMsNow
      let rep := walk tp pairs
      IO.println s!"coverage: {repr rep.cov}"
      IO.println s!"worst step ratio (permille of the contract): {rep.worstStepRatio}"
      IO.println s!"worst heap ratio (permille of the contract): {rep.worstHeapRatio}"
      IO.println s!"{(← IO.monoMsNow) - t0}ms"
      match rep.failure with
      | some f => IO.println s!"DISAGREEMENT: {f}"; pure 2
      | none => pure 0
