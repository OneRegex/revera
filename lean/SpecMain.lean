/-
speccheck: walk the corpus under the specification and report coverage and mismatches.
This is the diagnostic form of `Vego.specCorpusAgrees`.
It also reports the commands that take longest, to find what the budget should cover.
-/

import Vego.SpecCheck

open Vego

def main (args : List String) : IO UInt32 := do
  let path := args.headD "data/corpus.tsv"
  let budget := (args[1]?).bind String.toNat? |>.getD specBudget
  let txt ← IO.FS.readFile path
  match parseCorpus txt with
  | .error e => IO.eprintln e; pure 1
  | .ok pairs =>
    let sensible := sensiblePairs pairs
    let mut w : Walk := {}
    let mut bad : Array (Nat × String × String × Verdict) := #[]
    let mut idx := 0
    let mut slow : Array (Nat × String × Nat) := #[]
    let t0 ← IO.monoMsNow
    for (cmd, want) in sensible do
      let ts ← IO.monoMsNow
      let (v, w') := step budget w cmd
      w := w'
      let te ← IO.monoMsNow
      if te - ts > 500 then
        slow := slow.push (idx, cmd, te - ts)
        IO.println s!"slow #{idx} {te - ts}ms {cmd}"
      match v with
      | some v => if !v.holds want then
          bad := bad.push (idx, cmd, want, v)
          IO.println s!"MISMATCH #{idx} {cmd}\n    want {want}\n    spec {repr v}"
      | none => pure ()
      idx := idx + 1
      if idx % 10000 == 0 then
        IO.println s!"... {idx} commands, {(← IO.monoMsNow) - t0}ms"
    IO.println s!"coverage: {repr w.cov}"
    IO.println s!"mismatches: {bad.size}, slow: {slow.size}, total {(← IO.monoMsNow) - t0}ms"
    pure (if bad.isEmpty then 0 else 2)
