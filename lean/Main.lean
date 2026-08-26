/-
Command-line checker: replays a corpus of driver commands with
expected outputs (the tab-separated dump of crosscheck) against
the interpreted revera engine, and reports progress and the first
divergence. The theorems cover the embedded corpus under a fuel
budget; this executable scales the same check to any dump and any
budget.

Usage: vegocheck [corpus.tsv] [fuel] [limit]
-/

import Vego.Driver

open Vego

def main (args : List String) : IO UInt32 := do
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
      let mut s := { s0 with fuel }
      let mut checked := 0
      let mut skipped := 0
      let mut reStale := false
      let mut idx := 0
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
        let dtc := (← IO.monoMsNow) - tc
        if dtc > 3000 then
          IO.println s!"slow command {idx} ({dtc} ms): {cmd.take 60}"
          stdout.flush
        idx := idx + 1
        if idx % 2000 == 0 then
          let dt := (← IO.monoMsNow) - t0
          IO.println s!"{idx} commands, {dt} ms"
          stdout.flush
      let dt := (← IO.monoMsNow) - t0
      IO.println
        s!"done: {checked} agree, {skipped} skipped by fuel ({dt} ms)"
      return 0
