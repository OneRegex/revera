/-
The replay proposition the corpus theorem states.

`Vego.CorpusData` holds the corpus and the filter that drops the intractable executions.
This module adds the replay, which is the expensive part.
Do not import it from anything but the theorems: its closed terms are evaluated when the module loads.
-/

import Vego.CorpusData

namespace Vego

/--
Replay every command of `pairs` in one session and return the output line of each, in order.
Any fault, any run out of fuel, and any Exec past its resource contract is an error.
The session enforces the resource contracts, so a command whose Exec passes its contract fails the replay.
-/
def replayLines (tp : TProgram) (pairs : List (String × String)) : Except String (List String) := do
  let render : DriverErr → String := fun e =>
    match e with
    | .outOfFuel => "out of fuel"
    | .fault msg => msg
  let mut s ← (Session.start tp).mapError render
  let mut out : Array String := #[]
  let mut idx := 0
  for (cmd, _) in pairs do
    match s.eval cmd with
    | .error e => throw s!"command {idx} '{cmd}': {render e}"
    | .ok (got, s') =>
      out := out.push got
      s := s'.compact
    idx := idx + 1
  pure out.toList

/--
Replay `pairs` and report whether every command was answered exactly as the Go engine.
This is the corpus theorem's form: the replayed lines are the expected lines.
-/
def replayAgrees (pairs : List (String × String)) : Bool :=
  match reveraChecked with
  | .error _ => false
  | .ok tp =>
    match replayLines tp pairs with
    | .ok lines => lines == pairs.map (·.2) && lines.length > 0
    | .error _ => false

/--
Every command of the corpus agrees and stays within contract, except the executions of the two intractable patterns.

The coverage tests pin the filter down.
It must keep every compile command, so no pattern escapes the check.
It must also keep more than 98 percent of the corpus while it drops something.
A filter that stopped matching, or that matched everything, would fail the theorem rather than weaken it.

The `Unit` argument keeps this from being a closed term.
A closed term is evaluated when its module loads, and this one takes minutes.
-/
def corpusAgrees (_ : Unit) : Bool :=
  match corpusPairs with
  | .error _ => false
  | .ok pairs =>
    let sensible := sensiblePairs pairs
    countCompiles sensible == countCompiles pairs &&
    sensible.length * 100 > pairs.length * 98 &&
    sensible.length < pairs.length &&
    replayAgrees sensible

end Vego
