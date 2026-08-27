/-
A machine wraps a checked program with a live heap.
A host-side harness can then call functions and allocate cells for borrows and buffers.
It can also keep state between calls, the way the cross-language driver protocol does.
-/

import Vego.Interp

namespace Vego

structure Machine where
  prog : TProgram
  ctx : Ctx
  heap : Heap

namespace Machine

/-- Build a machine: evaluate the package-level variables. -/
def init (tp : TProgram) : Except Trap Machine := do
  let baseCtx : Ctx := { structs := tp.structs, funcs := tp.funcs,
                         globals := #[] }
  let mut globals : Array Val := #[]
  for ge in tp.globalInits do
    match evalExpr 1000000 baseCtx #[] ge Heap.empty with
    | .ok v _ => globals := globals.push v
    | .trap t => throw t
  pure { prog := tp,
         ctx := { baseCtx with globals },
         heap := Heap.empty }

def fnIdx? (m : Machine) (name : String) : Option Nat :=
  (m.prog.funcs.findIdx? (·.name == name))

def structIdx? (m : Machine) (name : String) : Option Nat :=
  m.prog.structNames.findIdx? (· == name)

/-- The zero value of a named struct. -/
def zeroStruct (m : Machine) (name : String) : Val :=
  match m.structIdx? name with
  | some si => zeroVal m.ctx.structs (.strukt si)
  | none => .b false

/--
Allocate a heap cell, for a borrowed struct or a buffer.
The harness never frees its cells, so their generation stays 0 and references to them carry generation 0.
-/
def alloc (m : Machine) (v : Val) : Machine × Nat :=
  ({ m with heap := { m.heap with cells := m.heap.cells.push (0, v) } },
   m.heap.cells.size)

def readCell (m : Machine) (cell : Nat) : Val :=
  (m.heap.cells.getD cell (0, .b false)).2

/-- Overwrite a harness-owned cell, keeping its generation. -/
def writeRoot (m : Machine) (cell : Nat) (v : Val) : Machine :=
  match m.heap.cells[cell]? with
  | some (g, _) =>
    { m with heap := { m.heap with cells := m.heap.cells.set! cell (g, v) } }
  | none => m

/-- Allocate a buffer holding the given elements and return the slice header for it. -/
def mkSlice (m : Machine) (elems : Array Val) : Machine × Val :=
  let (m', cell) := m.alloc (.arr elems)
  (m', .slice (some (cell, 0, [])) 0 elems.size elems.size)

/-- Read the current elements of a slice value out of the heap. -/
def sliceElems (m : Machine) (v : Val) : Option (Array Val) :=
  match v with
  | .slice none _ _ _ => some #[]
  | .slice (some (obj, _, path)) off len _ =>
    match (m.readCell obj).proj path with
    | .ok (.arr es) =>
      if off + len ≤ es.size then some (es.extract off (off + len))
      else none
    | _ => none
  | _ => none

/-- Clear the resource meter, so the next call is measured on its own. -/
def resetMeter (m : Machine) : Machine :=
  { m with heap := { m.heap with allocBytes := 0, steps := 0, loops := 0,
                                 depth := 0, maxDepth := 0 } }

def defaultFuel : Nat := 1000000000000

/--
Call a function by its resolved index: the same entry as in-language calls (`runFn`).
The heap keeps whatever the call allocated, so borrowed cells stay readable and session state persists.
-/
def callIdx (m : Machine) (idx : Nat) (args : List Val)
    (fuel : Nat := defaultFuel) : Except Trap (List Val × Machine) := do
  match m.prog.funcs[idx]? with
  | none => throw (.stuck "bad function index")
  | some fn =>
    match runFn fuel m.ctx fn args m.heap with
    | .ok vs h => pure (vs, { m with heap := h })
    | .trap t => throw t

/-- Call a function by name. -/
def call (m : Machine) (name : String) (args : List Val)
    (fuel : Nat := defaultFuel) : Except Trap (List Val × Machine) := do
  match m.fnIdx? name with
  | none => throw (.stuck s!"no function {name}")
  | some idx => m.callIdx idx args fuel

end Machine

end Vego
