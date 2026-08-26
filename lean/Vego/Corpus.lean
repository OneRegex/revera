/-
The embedded differential corpus, and the replay proposition the
corpus theorem states.

Two corpus patterns cannot be executed under the interpreter in
any reasonable time, so the theorem leaves their executions out.
Both nest a star inside counted repetitions:

    ((a*){250}){250}b     six blocks
    ((a*){4}){4}          six blocks

The parse search then explores a very large number of ways to
split a subject among nullable instances, and the cost comes from
the nesting rather than from the subject. Measured under the
interpreter, `((a*){250}){250}b` needs about a minute on the empty
subject and over an hour on a 120 byte one, and `((a*){4}){4}`
needs minutes on the empty subject. Replaying all twelve blocks
would take days.

What the theorem drops is exactly the X commands of those blocks,
1056 of 86691. It keeps their compile commands, so every pattern
in the corpus is still compiled and checked, and it keeps their T
commands, so the contract figures of those patterns are still
compared against the Go reference. That matters most for these
patterns, because their figures are the ones that reach the
saturation cap. Only the executions go.

Dropping X commands is sound for the session state. An X command
allocates its own match buffer and calls Exec; it writes no
session root, so the commands after it see exactly the session
they saw before.
-/

import Vego.Driver

namespace Vego

/-- The corpus with the expected output of the Go engine, tab
separated. -/
def corpusText : String := include_str "../data/corpus.tsv"

def corpusPairs : List (String × String) := parseCorpus corpusText

/-- The patterns whose executions the theorem leaves out, as the
hex the protocol carries:

    2828612a297b3235307d297b3235307d62   ((a*){250}){250}b
    2828612a297b347d297b347d             ((a*){4}){4}
-/
def intractablePatterns : List String :=
  ["2828612a297b3235307d297b3235307d62",
   "2828612a297b347d297b347d"]

/-- The tokens of a command, without the empty ones. -/
def cmdTokens (cmd : String) : List String :=
  (cmd.splitOn " ").filter (· ≠ "")

/-- The pattern token of a compile command, if the command is one. -/
def compilePattern (cmd : String) : Option String :=
  match cmdTokens cmd with
  | ["C", _, pat] => some pat
  | _ => none

def isCompile (cmd : String) : Bool := (cmdTokens cmd).head? == some "C"

def isExec (cmd : String) : Bool := (cmdTokens cmd).head? == some "X"

/-- Drop the executions of the intractable blocks. `inSlow` says
whether the block being scanned is one of them; a compile command
always starts a new block and is always kept. -/
def dropSlowExecs (pats : List String) (inSlow : Bool) :
    List (String × String) → List (String × String)
  | [] => []
  | (cmd, want) :: rest =>
    if isCompile cmd then
      let slow := match compilePattern cmd with
        | some pat => pats.contains pat
        | none => false
      (cmd, want) :: dropSlowExecs pats slow rest
    else if inSlow && isExec cmd then dropSlowExecs pats inSlow rest
    else (cmd, want) :: dropSlowExecs pats inSlow rest

/-- The corpus without those executions. -/
def sensiblePairs : List (String × String) :=
  dropSlowExecs intractablePatterns false corpusPairs

def countCompiles (pairs : List (String × String)) : Nat :=
  (pairs.filter (fun p => isCompile p.1)).length

/-- Replay `pairs` and report whether every command was checked
and answered exactly as the Go engine. The session enforces the
resource contracts, so a command whose Exec passes its contract
fails the replay. -/
def replayAgrees (pairs : List (String × String)) : Bool :=
  match reveraChecked with
  | .error _ => false
  | .ok tp =>
    match runCorpusFuel tp pairs with
    | .ok r => r.checked == pairs.length && r.skipped == 0 &&
               r.checked > 0
    | .error _ => false

/-- Every command of the corpus agrees and stays within contract,
except the executions of the two intractable patterns.

The coverage tests pin the filter down. It must keep every compile
command, so no pattern escapes the check, and it must keep more
than 98 percent of the corpus while dropping something. A filter
that stopped matching, or that matched everything, would fail the
theorem rather than weaken it. -/
def corpusAgrees : Bool :=
  countCompiles sensiblePairs == countCompiles corpusPairs &&
  sensiblePairs.length * 100 > corpusPairs.length * 98 &&
  sensiblePairs.length < corpusPairs.length &&
  replayAgrees sensiblePairs

end Vego
