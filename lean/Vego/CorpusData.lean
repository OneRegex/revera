/-
The embedded differential corpus and the replay set the theorems talk about.

This module holds only the data and the filter, which are cheap to evaluate.
The replay itself lives in `Vego.Corpus`, and a module that needs the corpus without the replay
imports this one: a closed top-level term is evaluated when its module loads, and the replay takes minutes.

Two corpus patterns cannot be executed under the interpreter in any reasonable time, so the theorem leaves their executions out.
Both nest a star inside counted repetitions:

    ((a*){250}){250}b     six blocks
    ((a*){4}){4}          six blocks

The parse search then explores a very large number of ways to split a subject among nullable instances.
The cost comes from the nesting, not from the subject.
Under the interpreter, `((a*){250}){250}b` needs about a minute on the empty subject.
It needs over an hour on a 120 byte one.
`((a*){4}){4}` needs minutes on the empty subject.
Replaying all twelve blocks would take days.

What the theorem drops is exactly the X commands of those blocks, 1056 of 86704.
It keeps their compile commands, so every pattern in the corpus is still compiled and checked.
It keeps their T commands, so the contract figures of those patterns are still compared against the Go reference.
That matters most for these patterns, because their figures are the largest the corpus produces.
Only the executions go.

Dropping X commands is sound for the session state.
An X command allocates its own match buffer and calls Exec.
It writes no session root, so the commands after it see exactly the session they saw before.
-/

import Vego.Driver

namespace Vego

/-- The corpus with the expected output of the Go engine, tab separated. -/
def corpusText : String := include_str "../data/corpus.tsv"

def corpusPairs : Except String (List (String × String)) := parseCorpus corpusText

/--
The patterns whose executions the theorem leaves out, as the hex the protocol carries:

    2828612a297b3235307d297b3235307d62   ((a*){250}){250}b
    2828612a297b347d297b347d             ((a*){4}){4}
-/
def intractablePatterns : List String :=
  ["2828612a297b3235307d297b3235307d62",
   "2828612a297b347d297b347d"]

/--
What one command is, for the filter: a compile carrying its pattern, an execution, or anything else.
One tokenization answers all three questions.
-/
inductive CmdKind where
  | compile (pat : Option String)
  | exec
  | other

def cmdKind (cmd : String) : CmdKind :=
  match (cmd.splitOn " ").filter (· ≠ "") with
  | ["C", _, pat] => .compile (some pat)
  | "C" :: _ => .compile none
  | "X" :: _ => .exec
  | _ => .other

def isCompile (cmd : String) : Bool :=
  match cmdKind cmd with
  | .compile _ => true
  | _ => false

/--
Drop the executions of the intractable blocks.
`inSlow` says whether the block being scanned is one of them.
A compile command always starts a new block, and it always stays.
The accumulator keeps the recursion flat: the corpus is long enough that a non-tail recursion here overflows the Lean interpreter.
-/
def dropSlowExecs (pats : List String) (inSlow : Bool)
    (acc : List (String × String)) :
    List (String × String) → List (String × String)
  | [] => acc.reverse
  | (cmd, want) :: rest =>
    match cmdKind cmd with
    | .compile pat =>
      let slow := match pat with
        | some p => pats.contains p
        | none => false
      dropSlowExecs pats slow ((cmd, want) :: acc) rest
    | .exec =>
      if inSlow then dropSlowExecs pats inSlow acc rest
      else dropSlowExecs pats inSlow ((cmd, want) :: acc) rest
    | .other => dropSlowExecs pats inSlow ((cmd, want) :: acc) rest

/-- The corpus without those executions. -/
def sensiblePairs (pairs : List (String × String)) : List (String × String) :=
  dropSlowExecs intractablePatterns false [] pairs

def countCompiles (pairs : List (String × String)) : Nat :=
  pairs.countP (fun p => isCompile p.1)

end Vego
