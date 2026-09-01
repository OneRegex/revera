/-
The interpreted engine against the specification on an exhaustive small domain.

The corpus theorems reach the specification through the recorded reference outputs. This module compares
the two directly, on inputs no engine has seen: every token string of the small pattern language below,
every subject over `{a, b}` up to a length, and a few flag settings. Each defined pattern is compiled under
the formal Vego semantics and executed on every subject, and the output line must be the line the
specification requires.

The domain is small, so the enumeration is complete rather than sampled: every pattern of the token
language up to the length bound is in it.
-/

import Vego.Driver
import Vego.SpecCheck

namespace Vego

open Ere

/-- Words of exactly `n` tokens. -/
def words (toks : List String) : Nat → List String
  | 0 => [""]
  | n + 1 => (words toks n).flatMap fun w => toks.map (w ++ ·)

/-- Words of one through `n` tokens. -/
def wordsUpTo (toks : List String) (n : Nat) : List String :=
  (List.range n).flatMap fun k => words toks (k + 1)

/-- The token alphabet: atoms, groups, alternation, every duplication form, both anchors. -/
def tokensWide : List String :=
  ["a", "b", ".", "(", ")", "|", "*", "+", "?", "^", "$", "[ab]", "[^a]", "{0,1}"]

/-- One sweep: the patterns, the subjects, and the flag settings it crosses. -/
structure Sweep where
  patterns : List String
  subjects : List String
  cflags : List Nat
  eflags : List Nat

/--
The structure sweep: every pattern of one through four tokens, every subject over `{a, b}` of up to three
characters and the empty subject, with and without `REG_MINIMAL`, with and without both `REG_NOTBOL` and
`REG_NOTEOL`.
-/
def sweepStructure : Sweep :=
  { patterns := wordsUpTo tokensWide 4
    subjects := wordsUpTo ["a", "b"] 3 ++ [""]
    cflags := [0, 8]
    eflags := [0, 3] }

/--
The flag sweep: every pattern of one through three tokens, under all sixteen compile flag combinations
of `REG_ICASE`, `REG_NEWLINE`, `REG_NOSUB` and `REG_MINIMAL`, and all four execution flag combinations.
The subjects add newlines and uppercase letters, which are what those flags act on.
-/
def sweepFlags : Sweep :=
  { patterns := wordsUpTo tokensWide 3
    subjects := wordsUpTo ["a", "b"] 2 ++ ["", "\n", "a\nb", "A", "aB", "b\n"]
    cflags := List.range 16
    eflags := List.range 4 }

structure ExhCoverage where
  patterns : Nat := 0
  compiles : Nat := 0
  execs : Nat := 0
  overBudget : Nat := 0
  deriving Repr, BEq, DecidableEq, Inhabited

/--
Run the sweep in one interpreted driver session.
Every defined pattern must compile with the specification's subexpression count, and every execution
must print the line the specification requires. The first disagreement is the error.
-/
def exhaustiveRun (tp : TProgram) (sw : Sweep) (budget : Nat) : Except String ExhCoverage := do
  let render : DriverErr → String := fun e =>
    match e with
    | .outOfFuel => "out of fuel"
    | .fault msg => msg
  let mut s ← (Session.start tp).mapError render
  let mut cov : ExhCoverage := {}
  for pat in sw.patterns do
    cov := { cov with patterns := cov.patterns + 1 }
    for cf in sw.cflags do
      let flags := CFlags.ofBits cf
      match parsePattern posixLocale flags pat.toUTF8 with
      | .defined e nsub =>
        let ccmd := s!"C {cf} {tokEncode pat.toUTF8}"
        match s.eval ccmd with
        | .error err => throw s!"{ccmd}: {render err}"
        | .ok (got, s') =>
          s := s'.compact
          if got != s!"C 0 0 {nsub}" then throw s!"{ccmd}: engine '{got}', spec nsub {nsub}"
          cov := { cov with compiles := cov.compiles + 1 }
          for subj in sw.subjects do
            let some subject := Subject.ofBytes subj.toUTF8 | throw "subject outside domain"
            for ef in sw.eflags do
              let ctx : Ere.Ctx := { loc := posixLocale, flags, eflags := EFlags.ofBits ef, subj := subject }
              match exec ctx e nsub budget with
              | none => cov := { cov with overBudget := cov.overBudget + 1 }
              | some out =>
                let want := renderOutcome out
                let xcmd := s!"X {ef} {tokEncode subj.toUTF8}"
                match s.eval xcmd with
                | .error err => throw s!"{ccmd} then {xcmd}: {render err}"
                | .ok (got, s') =>
                  s := s'.compact
                  if got != want then throw s!"{ccmd} then {xcmd}: engine '{got}', spec '{want}'"
                  cov := { cov with execs := cov.execs + 1 }
      | _ => pure ()
  pure cov

/-- The coverage of the structure sweep, which the theorem pins down. -/
def coverageStructure : ExhCoverage :=
  { patterns := 41370, compiles := 24736, execs := 742080, overBudget := 0 }

/-- The coverage of the flag sweep, which the theorem pins down. -/
def coverageFlags : ExhCoverage :=
  { patterns := 2954, compiles := 17392, execs := 834816, overBudget := 0 }

/-- The interpreted engine meets the specification on both sweeps, at the pinned coverage. -/
def exhaustiveAgrees (_ : Unit) : Bool :=
  match reveraChecked with
  | .error _ => false
  | .ok tp =>
    match exhaustiveRun tp sweepStructure specBudget, exhaustiveRun tp sweepFlags specBudget with
    | .ok a, .ok b => a == coverageStructure && b == coverageFlags
    | _, _ => false

end Vego
