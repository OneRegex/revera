/-
The machine-checked statements.

Every theorem here is about the exact JSON artifacts that the Go,
Rust, Zig and C++ printers consume, embedded byte for byte, and
about the formal Vego semantics of Interp.lean. They are proved by
native evaluation (`native_decide`), so the trusted base is the
Lean kernel plus the Lean compiler.

What the theorems say:

1. `probe_wellformed` and `revera_wellformed`: both programs parse
   from their JSON and elaborate into the typed core. Elaboration
   replays name resolution, operator typing, constant folding and
   width assignment, so success is a well-formedness proof.

2. `probe_agrees`: under the formal semantics, the probe program
   produces, line for line, the reference output of the Go
   original for the whole probe call matrix. The probe matrix
   pins the semantic corners: division overflow, wrapping at all
   widths, evaluation order, spare-capacity zeroing, nil-ness,
   range semantics, struct equality, and view writes.

3. `revera_corpus_agrees`: under the formal semantics, the revera
   engine answers every command of the embedded crosscheck corpus
   with exactly the output of the Go reference engine, and it does
   so without hitting any trap: no out-of-range index or slice, no
   division by zero, no impossible shift, anywhere in those runs.
   Checking this theorem replays the whole corpus through the
   interpreter and takes tens of minutes; the built module caches
   the result.
-/

import Vego.Probe
import Vego.Driver

namespace Vego

/-- The embedded differential corpus: crosscheck commands with the
expected output of the Go engine, tab separated. -/
def corpusText : String := include_str "../data/corpus.tsv"

def corpusPairs : List (String × String) :=
  parseCorpus corpusText

/-- True when the interpreted engine reproduces the whole corpus:
every command answers exactly as the Go engine and nothing traps.
Checking this proposition replays all 86691 commands through the
interpreter, which takes a while; the result is cached in the
built module. -/
def corpusAgrees : Bool :=
  match reveraChecked with
  | .error _ => false
  | .ok tp =>
    match runCorpusFuel tp corpusPairs with
    | .ok r => r.checked == corpusPairs.length && r.skipped == 0 &&
               r.checked > 0
    | .error _ => false

theorem probe_wellformed : probeChecked.isOk = true := by native_decide

theorem revera_wellformed : reveraChecked.isOk = true := by native_decide

theorem probe_agrees : probeAgrees = true := by native_decide

theorem revera_corpus_agrees : corpusAgrees = true := by native_decide

end Vego
