/-
The operational semantics of Vego, as a definitional interpreter
over the typed core.

The memory model is a heap of cells. Every local variable lives in
one cell, and every buffer produced by make, append, a slice
literal, or a byte conversion lives in its own cell. A slice value
is a header: a cell, a path to an array inside that cell, an
offset, a length, and a capacity. Views of a local array point
into the array's cell, so writes through a view and writes to the
variable see each other, exactly as in Go.

Everything is total: the interpreter recurses on an explicit fuel
argument and reports exhaustion as a trap. The other traps are the
abnormal terminations of the specification: out-of-range index or
slice, division by zero, and an impossible shift. `stuck` marks a
state the typed core can never reach; reaching it would mean the
elaborator let an ill-typed program through.

Buffer growth follows the portable runtime contract implemented by
the Zig, C++ and Rust runtimes: a growing append allocates
max(2 * cap, 8, need) elements and zero-fills the spare region.
-/

import Vego.Core
import Vego.Elab

namespace Vego

inductive Trap where
  | oob
  | divZero
  | shiftRange
  | makeBad
  | fuel
  | stale
  | stuck (msg : String)
  deriving Repr, BEq

/-- A path inside a cell: field or element positions. -/
abbrev Path := List Nat

/-- A location: cell, generation, path. The generation must match
the cell's current generation, so a reference into a freed frame
traps instead of reading whatever lives there next. -/
abbrev Loc := Nat × Nat × Path

inductive Val where
  | i (v : Int)
  | b (v : Bool)
  | s (v : ByteArray)
  | slice (base : Option Loc) (off : Nat) (len : Nat) (cap : Nat)
  | arr (elems : Array Val)
  | strukt (fields : Array Val)
  | ptr (obj : Nat) (gen : Nat) (path : Path)
  deriving Repr

instance : Inhabited Val := ⟨.b false⟩

/-- A heap cell: its current generation and its value. Freeing a
cell bumps the generation and pushes the id on the free list, so
every function call can recycle its frame cells on return. Cell 0
is a permanent dummy; frames use id 0 as the unset sentinel. -/
abbrev Cell := Nat × Val

structure Heap where
  cells : Array Cell
  free : Array Nat
  /- The resource meter. It measures what a run of the interpreted
  program costs in the units of the resource contract: bytes of
  buffer allocations, the depth of the call stack, and abstract
  steps. Harnesses reset it before a call and read it after.
  `steps` counts statements, loop iterations, and calls. `loops`
  counts only loop iterations and calls; the straight-line code
  between two of those ticks is bounded by the program text, so
  either counter bounds the total work up to a program constant. -/
  allocBytes : Nat := 0
  steps : Nat := 0
  loops : Nat := 0
  depth : Nat := 0
  maxDepth : Nat := 0

def Heap.empty : Heap := { cells := #[(0, .b false)], free := #[] }

/-- Interpreter results: a value with the heap, or a trap. -/
inductive Res (α : Type) where
  | ok (v : α) (h : Heap)
  | trap (t : Trap)

def M (α : Type) := Heap → Res α

@[always_inline]
instance : Monad M where
  pure v := fun h => .ok v h
  bind x f := fun h =>
    match x h with
    | .ok v h' => f v h'
    | .trap t => .trap t

def M.trap {α : Type} (t : Trap) : M α := fun _ => .trap t

/-- Allocate a cell, reusing a freed one when possible. The
returned generation is the one references must carry. -/
def M.alloc (v : Val) : M (Nat × Nat) := fun h =>
  match h.free.back? with
  | some id =>
    match h.cells[id]? with
    | some (g, _) =>
      .ok (id, g) { h with cells := h.cells.set! id (g, v),
                           free := h.free.pop }
    | none => .trap (.stuck "bad free list")
  | none =>
    .ok (h.cells.size, 0) { h with cells := h.cells.push (0, v) }

/-- Free one cell: bump the generation, drop the value, recycle. -/
def M.freeCell (id : Nat) : M Unit := fun h =>
  match h.cells[id]? with
  | some (g, _) =>
    .ok () { h with cells := h.cells.set! id (g + 1, .b false),
                    free := h.free.push id }
  | none => .trap (.stuck "bad cell free")

/-- Meter ticks. `tickStmt` counts one statement; `tickLoop`
counts one loop iteration or one call, which also counts as a
statement-level step. -/
@[always_inline]
def M.tickStmt : M Unit := fun h =>
  .ok () { h with steps := h.steps + 1 }

@[always_inline]
def M.tickLoop : M Unit := fun h =>
  .ok () { h with steps := h.steps + 1, loops := h.loops + 1 }

/-- Count one buffer allocation of the given size, in bytes. -/
@[always_inline]
def M.charge (bytes : Nat) : M Unit := fun h =>
  .ok () { h with allocBytes := h.allocBytes + bytes }

/-- Enter and leave one function call, tracking the deepest chain.
A trap abandons the whole run, so a missed `exitFn` on a trap path
cannot skew a completed measurement. -/
@[always_inline]
def M.enterFn : M Unit := fun h =>
  let d := h.depth + 1
  .ok () { h with steps := h.steps + 1, loops := h.loops + 1,
                  depth := d, maxDepth := Nat.max h.maxDepth d }

@[always_inline]
def M.exitFn : M Unit := fun h =>
  .ok () { h with depth := h.depth - 1 }

/- Byte sizes of the values a buffer allocation holds. The layout
is the natural 64-bit layout every target shares: one byte for
bool and u8, the declared width for the other integers, an 8-byte
pointer, a 16-byte string header, and a 24-byte slice header.
Struct fields are aligned to their natural alignment and the
struct size is rounded up to the struct alignment, as Go and the
generated targets lay them out. The depth argument only bounds the
recursion through the struct table, like in `zeroValD`. -/
def IW.byteSize : IW → Nat
  | .u8 => 1
  | .u16 => 2
  | .u32 => 4
  | .u64 => 8
  | .i32 => 4
  | .i64 => 8

def sizeAlignD (depth : Nat) (structs : Array (Array VTy)) :
    VTy → Nat × Nat
  | .bool => (1, 1)
  | .int w => (w.byteSize, w.byteSize)
  | .str => (16, 8)
  | .slice _ => (24, 8)
  | .ptr _ => (8, 8)
  | .arr n e =>
    match depth with
    | 0 => (0, 1)
    | d + 1 =>
      let (s, a) := sizeAlignD d structs e
      (n * s, a)
  | .strukt si =>
    match depth with
    | 0 => (0, 1)
    | d + 1 =>
      match structs[si]? with
      | none => (0, 1)
      | some ftys =>
        let oa := ftys.toList.foldl
          (fun acc fty =>
            let sa := sizeAlignD d structs fty
            (((acc.1 + sa.2 - 1) / sa.2) * sa.2 + sa.1,
             Nat.max acc.2 sa.2))
          (0, 1)
        (((oa.1 + oa.2 - 1) / oa.2) * oa.2, oa.2)

/-- The allocation size of one buffer element of the given type. -/
def elemBytes (structs : Array (Array VTy)) (ty : VTy) : Nat :=
  (sizeAlignD 10000 structs ty).1

/-- Unchecked cell read, for cells the caller owns: frame slots,
loop cells, and harness roots, which are never freed while held. -/
def M.readCell (i : Nat) : M Val := fun h =>
  match h.cells[i]? with
  | some (_, v) => .ok v h
  | none => .trap (.stuck "bad cell")

def M.cellGen (i : Nat) : M Nat := fun h =>
  match h.cells[i]? with
  | some (g, _) => .ok g h
  | none => .trap (.stuck "bad cell")

def M.writeCell (i : Nat) (v : Val) : M Unit := fun h =>
  match h.cells[i]? with
  | some (g, _) => .ok () { h with cells := h.cells.set! i (g, v) }
  | none => .trap (.stuck "bad cell write")

/-- Project a value along a path. -/
def Val.proj (v : Val) (path : Path) : Except Trap Val :=
  match path with
  | [] => .ok v
  | k :: rest =>
    match v with
    | .arr es | .strukt es =>
      match es[k]? with
      | some e => e.proj rest
      | none => .error (.stuck "bad path")
    | _ => .error (.stuck "bad path base")

/-- Functionally update a value along a path. The touched slot is
detached before the recursive update, so a uniquely referenced
value updates in place instead of copying every array on the
path. -/
def Val.store (v : Val) (path : Path) (nv : Val) : Except Trap Val :=
  match path with
  | [] => .ok nv
  | k :: rest =>
    match v with
    | .arr es =>
      match es[k]? with
      | some e =>
        let es := es.set! k (.b false)
        do pure (.arr (es.set! k (← e.store rest nv)))
      | none => .error (.stuck "bad path")
    | .strukt es =>
      match es[k]? with
      | some e =>
        let es := es.set! k (.b false)
        do pure (.strukt (es.set! k (← e.store rest nv)))
      | none => .error (.stuck "bad path")
    | _ => .error (.stuck "bad path base")

/-- Read through a location, checking the generation. -/
def M.readLoc (obj : Nat) (gen : Nat) (path : Path) : M Val := fun h =>
  match h.cells[obj]? with
  | some (g, v) =>
    if g != gen then .trap .stale
    else
      match v.proj path with
      | .ok r => .ok r h
      | .error t => .trap t
  | none => .trap (.stuck "bad cell")

/-- Write through a location. The generation is checked, the cell
is detached first, and the one- and two-step paths that carry
nearly all traffic are updated inline, so a uniquely referenced
cell mutates in place instead of copying its arrays. -/
def M.writeLoc (obj : Nat) (gen : Nat) (path : Path) (nv : Val) :
    M Unit := fun h0 =>
  match h0.cells[obj]? with
  | none => .trap (.stuck "bad cell")
  | some (g, v) =>
    if g != gen then .trap .stale else
    let hc := h0.cells.set! obj (g, .b false)
    let put (v' : Val) : Res Unit :=
      .ok () { h0 with cells := hc.set! obj (g, v') }
    match path, v with
    | [], _ => put nv
    | [k], .arr es =>
      if k < es.size then put (.arr (es.set! k nv))
      else .trap (.stuck "bad path")
    | [k], .strukt es =>
      if k < es.size then put (.strukt (es.set! k nv))
      else .trap (.stuck "bad path")
    | [k1, k2], .arr es =>
      (match es[k1]? with
       | some e =>
         let es := es.set! k1 (.b false)
         match e.store [k2] nv with
         | .ok e' => put (.arr (es.set! k1 e'))
         | .error t => .trap t
       | none => .trap (.stuck "bad path"))
    | [k1, k2], .strukt es =>
      (match es[k1]? with
       | some e =>
         let es := es.set! k1 (.b false)
         match e.store [k2] nv with
         | .ok e' => put (.strukt (es.set! k1 e'))
         | .error t => .trap t
       | none => .trap (.stuck "bad path"))
    | _, _ =>
      match v.store path nv with
      | .ok v' => put v'
      | .error t => .trap t

/-- Zero values. The depth argument only bounds the recursion
through the struct table; real programs never come close. -/
def zeroValD (depth : Nat) (structs : Array (Array VTy)) : VTy → Val
  | .bool => .b false
  | .int _ => .i 0
  | .str => .s ByteArray.empty
  | .slice _ => .slice none 0 0 0
  | .arr n e =>
    match depth with
    | 0 => .b false
    | d + 1 => .arr (Array.replicate n (zeroValD d structs e))
  | .strukt si =>
    match depth with
    | 0 => .b false
    | d + 1 =>
      match structs[si]? with
      | some ftys => .strukt (ftys.map (zeroValD d structs))
      | none => .b false
  | .ptr _ => .b false

def zeroVal (structs : Array (Array VTy)) (ty : VTy) : Val :=
  zeroValD 10000 structs ty

/-- Wrapping helpers on canonical integers. -/
def toUnsigned (w : IW) (v : Int) : Nat :=
  (IW.wrap (match w with
    | .i32 => .u32 | .i64 => .u64 | x => x) v).toNat

def applyArith (op : ArithOp) (w : IW) (a b : Int) : Except Trap Int :=
  match op with
  | .add => .ok (w.wrap (a + b))
  | .sub => .ok (w.wrap (a - b))
  | .mul => .ok (w.wrap (a * b))
  | .quo =>
    if b == 0 then .error .divZero else .ok (w.wrap (Int.tdiv a b))
  | .rem =>
    if b == 0 then .error .divZero else .ok (w.wrap (Int.tmod a b))
  | .band => .ok (w.wrap (toUnsigned w a &&& toUnsigned w b))
  | .bor => .ok (w.wrap (toUnsigned w a ||| toUnsigned w b))
  | .bxor => .ok (w.wrap (toUnsigned w a ^^^ toUnsigned w b))
  | .andNot =>
    let ub := toUnsigned w b
    let mask := (w.modulus - 1).toNat
    .ok (w.wrap (toUnsigned w a &&& (ub ^^^ mask)))

def applyShift (left : Bool) (w : IW) (a : Int) (count : Int) :
    Except Trap Int :=
  if count < 0 || count ≥ w.bits then .error .shiftRange
  else
    let c := count.toNat
    if left then .ok (w.wrap (a * Int.ofNat (1 <<< c)))
    else if w.signed then .ok (Int.fdiv a (Int.ofNat (1 <<< c)))
    else .ok (Int.ofNat (a.toNat >>> c))

mutual

/-- Structural equality for scalars, strings, arrays and structs. -/
def valEq (fuel : Nat) (a b : Val) : Except Trap Bool :=
  match fuel with
  | 0 => .error .fuel
  | fuel + 1 =>
    match a, b with
    | .i x, .i y => .ok (x == y)
    | .b x, .b y => .ok (x == y)
    | .s x, .s y => .ok (strCompare x y == .eq)
    | .arr xs, .arr ys | .strukt xs, .strukt ys =>
      if xs.size != ys.size then .ok false
      else valEqArr fuel xs ys 0
    | _, _ => .error (.stuck "uncomparable values")

def valEqArr (fuel : Nat) (xs ys : Array Val) (i : Nat) :
    Except Trap Bool :=
  match fuel with
  | 0 => .error .fuel
  | fuel + 1 =>
    if h : i < xs.size then
      if h2 : i < ys.size then
        match valEq fuel xs[i] ys[i] with
        | .ok true => valEqArr fuel xs ys (i + 1)
        | r => r
      else .ok false
    else .ok true

end

/-- Execution context: the checked program plus evaluated globals. -/
structure Ctx where
  structs : Array (Array VTy)
  funcs : Array TFunc
  globals : Array Val

/-- Control flow out of a statement. -/
inductive Flow where
  | normal
  | brk
  | cont
  | retv (vs : List Val)

/-- A function frame maps slots to cells; 0 is the unset sentinel
(cell 0 is a permanent dummy). -/
abbrev Frame := Array Nat

/-- Free every cell a frame owns. Locals and parameters cannot
legally outlive their call, so this runs at every return. -/
def M.freeFrameFrom (fr : Frame) (i : Nat) : M Unit := do
  if h : i < fr.size then
    if fr[i] != 0 then do
      M.freeCell fr[i]
      M.freeFrameFrom fr (i + 1)
    else M.freeFrameFrom fr (i + 1)
  else pure ()
termination_by fr.size - i

def M.freeFrame (fr : Frame) : M Unit :=
  M.freeFrameFrom fr 0

def M.expectInt (v : Val) : M Int :=
  match v with
  | .i n => pure n
  | _ => M.trap (.stuck "expected an integer")

def M.expectBool (v : Val) : M Bool :=
  match v with
  | .b x => pure x
  | _ => M.trap (.stuck "expected a bool")

def M.expectStr (v : Val) : M ByteArray :=
  match v with
  | .s x => pure x
  | _ => M.trap (.stuck "expected a string")

def M.expectSlice (v : Val) : M (Option Loc × Nat × Nat × Nat) :=
  match v with
  | .slice base off len cap => pure (base, off, len, cap)
  | _ => M.trap (.stuck "expected a slice")

/-- Read one buffer element through a slice header. -/
def M.readElem (base : Option Loc) (off : Nat) (k : Nat) : M Val := do
  match base with
  | none => M.trap (.stuck "read from an empty buffer")
  | some (obj, gen, path) =>
    match ← M.readLoc obj gen path with
    | .arr es =>
      match es[off + k]? with
      | some v => pure v
      | none => M.trap (.stuck "buffer read out of range")
    | _ => M.trap (.stuck "buffer base is not an array")

/-- Write one buffer element through a slice header. -/
def M.writeElem (base : Option Loc) (off : Nat) (k : Nat)
    (nv : Val) : M Unit := do
  match base with
  | none => M.trap (.stuck "write into an empty buffer")
  | some (obj, gen, path) => M.writeLoc obj gen (path ++ [off + k]) nv

/-- Copy vs into es starting at base, purely. -/
def blitInto (es : Array Val) (base : Nat) (vs : Array Val) :
    Array Val :=
  vs.size.fold (fun j _ acc => acc.set! (base + j) vs[j]!) es

/-- Bulk-write values into a buffer starting at element k: one cell
read-modify-write instead of one per element. -/
def M.writeElems (base : Option Loc) (off : Nat) (k : Nat)
    (vs : Array Val) : M Unit := do
  if vs.isEmpty then pure ()
  else
    match base with
    | none => M.trap (.stuck "write into an empty buffer")
    | some (obj, gen, path) => do
      match ← M.readLoc obj gen path with
      | .arr es => do
        if off + k + vs.size > es.size then
          M.trap (.stuck "buffer write out of range")
        else
          M.writeLoc obj gen path (.arr (blitInto es (off + k) vs))
      | _ => M.trap (.stuck "buffer base is not an array")

/-- Bulk-read the live elements of a slice header. -/
def M.readElems (base : Option Loc) (off : Nat) (len : Nat) :
    M (Array Val) := do
  if len == 0 then pure #[]
  else
    match base with
    | none => M.trap (.stuck "read from an empty buffer")
    | some (obj, gen, path) => do
      match ← M.readLoc obj gen path with
      | .arr es =>
        if off + len > es.size then
          M.trap (.stuck "buffer read out of range")
        else pure (es.extract off (off + len))
      | _ => M.trap (.stuck "buffer base is not an array")

/-- The Val image of a byte string. -/
def bytesToVals (s : ByteArray) : Array Val :=
  s.data.map (fun b => .i b.toNat)

/-- Gather the elements a spread append or a copy reads: a snapshot,
which is what makes both memmove-safe. -/
def M.gatherSrc (sv : Val) (srcIsStr : Bool) : M (Array Val) := do
  if srcIsStr then do
    pure (bytesToVals (← M.expectStr sv))
  else do
    let (base, off, len, _) ← M.expectSlice sv
    M.readElems base off len

/-- Bind a fresh cell holding v into a frame slot. -/
def M.bindSlot (fr : Frame) (slot : Nat) (v : Val) :
    M (Frame × Nat) := do
  let (cell, _) ← M.alloc v
  pure (fr.set! slot cell, cell)

/-- Free the old cell in a frame slot, if any, and bind a fresh one
holding v. -/
def M.rebindSlot (fr : Frame) (slot : Nat) (v : Val) :
    M (Frame × Nat) := do
  match fr[slot]? with
  | some old =>
    if old != 0 then do
      M.freeCell old
      M.bindSlot fr slot v
    else M.bindSlot fr slot v
  | none => M.bindSlot fr slot v

/-- The growth rule of the portable append contract, which the
Zig, C++ and Rust runtimes implement. The cost lemmas reason about
this definition, so the rule has one home. -/
def growCap (cap need : Nat) : Nat :=
  Nat.max (Nat.max (2 * cap) 8) need

/-- The append primitive: in place inside capacity, else a grown
buffer under the portable contract, with a zeroed spare region. -/
def doAppend (c : Ctx) (sv : Val) (adds : Array Val)
    (elemTy : VTy) : M Val := do
  let (base, off, len, cap) ← M.expectSlice sv
  let need := len + adds.size
  if adds.size == 0 then pure sv
  else if need ≤ cap then do
    M.writeElems base off len adds
    pure (.slice base off need cap)
  else do
    let newcap := growCap cap need
    M.charge (newcap * elemBytes c.structs elemTy)
    let live ← M.readElems base off len
    let buf := live ++ adds ++
      Array.replicate (newcap - need) (zeroVal c.structs elemTy)
    let (cell, g) ← M.alloc (.arr buf)
    pure (.slice (some (cell, g, [])) 0 need newcap)

/-- The make primitive: a zeroed buffer of the requested length
and capacity. -/
def doMake (c : Ctx) (elemTy : VTy) (n cp : Int) : M Val := do
  if n < 0 || cp < n then M.trap .makeBad
  else do
    M.charge (cp.toNat * elemBytes c.structs elemTy)
    let buf : Array Val :=
      Array.replicate cp.toNat (zeroVal c.structs elemTy)
    let (cell, g) ← M.alloc (.arr buf)
    pure (.slice (some (cell, g, [])) 0 n.toNat cp.toNat)

/-- The slice-literal primitive: a fresh exact-size buffer holding
the evaluated elements. -/
def doSliceLit (c : Ctx) (elemTy : VTy) (vs : Array Val) : M Val := do
  M.charge (vs.size * elemBytes c.structs elemTy)
  let (cell, g) ← M.alloc (.arr vs)
  pure (.slice (some (cell, g, [])) 0 vs.size vs.size)

/-- The string-to-bytes conversion: a fresh buffer with one cell
per byte. -/
def doStrToBytes (s : ByteArray) : M Val := do
  M.charge s.size
  let (cell, g) ← M.alloc (.arr (bytesToVals s))
  pure (.slice (some (cell, g, [])) 0 s.size s.size)

/-- The byte image of buffer elements, purely; none on a stray
non-integer, which the typed core rules out. -/
def valsToBytes (vs : Array Val) : Option ByteArray :=
  vs.foldl
    (fun acc v =>
      match acc, v with
      | some out, .i b => some (out.push (UInt8.ofNat b.toNat))
      | _, _ => none)
    (some (ByteArray.emptyWithCapacity vs.size))

/-- The bytes-to-string conversion: the string storage counts as
one allocation of its length. -/
def doBytesToStr (vs : Array Val) : M Val := do
  M.charge vs.size
  match valsToBytes vs with
  | some out => pure (.s out)
  | none => M.trap (.stuck "byte buffer holds a non-integer")

/-- One loop-variable cell, rebound per loop statement. -/
def allocLoopCell (fr : Frame) (slot : Option Nat) :
    M (Frame × Option Nat) := do
  match slot with
  | none => pure (fr, none)
  | some s => do
    let (fr', cell) ← M.rebindSlot fr s (.b false)
    pure (fr', some cell)

mutual

/-- Resolve a place to a location, evaluating its index
expressions in source order. -/
def evalPlace (fuel : Nat) (c : Ctx) (fr : Frame) :
    TPlace → M Loc
  | .localP slot => do
    match fuel with
    | 0 => M.trap .fuel
    | _ =>
      match fr[slot]? with
      | some cell =>
        if cell == 0 then M.trap (.stuck "unset slot")
        else do
          let g ← M.cellGen cell
          pure (cell, g, [])
      | none => M.trap (.stuck "unset slot")
  | .fieldP base i viaPtr => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (obj, g, path) ← evalPlace fuel c fr base
      if viaPtr then
        match ← M.readLoc obj g path with
        | .ptr tobj tgen tpath => pure (tobj, tgen, tpath ++ [i])
        | _ => M.trap (.stuck "deref of a non-pointer")
      else
        pure (obj, g, path ++ [i])
  | .indexArrP base n idx => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (obj, g, path) ← evalPlace fuel c fr base
      let iv ← M.expectInt (← evalExpr fuel c fr idx)
      if iv < 0 || iv ≥ n then M.trap .oob
      else pure (obj, g, path ++ [iv.toNat])
  | .indexSliceP sliceVal idx => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (base, off, len, _) ←
        M.expectSlice (← evalExpr fuel c fr sliceVal)
      let iv ← M.expectInt (← evalExpr fuel c fr idx)
      if iv < 0 || iv ≥ len then M.trap .oob
      else
        match base with
        | some (obj, gen, path) => pure (obj, gen, path ++ [off + iv.toNat])
        | none => M.trap (.stuck "element of an empty buffer")

def evalExprs (fuel : Nat) (c : Ctx) (fr : Frame) :
    List TExpr → M (List Val)
  | [] => pure []
  | e :: rest => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let v ← evalExpr fuel c fr e
      let vs ← evalExprs fuel c fr rest
      pure (v :: vs)

def evalExpr (fuel : Nat) (c : Ctx) (fr : Frame) : TExpr → M Val
  | .litInt v => pure (.i v)
  | .litBool v => pure (.b v)
  | .litStr v => pure (.s v)
  | .zeroOf ty => pure (zeroVal c.structs ty)
  | .globalGet idx =>
    match c.globals[idx]? with
    | some v => pure v
    | none => M.trap (.stuck "bad global")
  | .placeGet (.localP slot) => do
    match fuel with
    | 0 => M.trap .fuel
    | _ =>
      match fr[slot]? with
      | some cell =>
        if cell == 0 then M.trap (.stuck "unset slot")
        else M.readCell cell
      | none => M.trap (.stuck "unset slot")
  | .placeGet p => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (obj, g, path) ← evalPlace fuel c fr p
      M.readLoc obj g path
  | .fieldGet x i => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      match ← evalExpr fuel c fr x with
      | .strukt fs =>
        match fs[i]? with
        | some v => pure v
        | none => M.trap (.stuck "bad field")
      | .ptr obj gen path => do
        match (← M.readLoc obj gen path) with
        | .strukt fs =>
          match fs[i]? with
          | some v => pure v
          | none => M.trap (.stuck "bad field")
        | _ => M.trap (.stuck "pointer to a non-struct")
      | _ => M.trap (.stuck "field of a non-struct")
  | .indexSliceGet x idx => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (base, off, len, _) ← M.expectSlice (← evalExpr fuel c fr x)
      let iv ← M.expectInt (← evalExpr fuel c fr idx)
      if iv < 0 || iv ≥ len then M.trap .oob
      else M.readElem base off iv.toNat
  | .indexStrGet x idx => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let s ← M.expectStr (← evalExpr fuel c fr x)
      let iv ← M.expectInt (← evalExpr fuel c fr idx)
      if iv < 0 || iv ≥ s.size then M.trap .oob
      else pure (.i (s[iv.toNat]!.toNat))
  | .indexArrVal x idx => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let xv ← evalExpr fuel c fr x
      let iv ← M.expectInt (← evalExpr fuel c fr idx)
      match xv with
      | .arr es =>
        if iv < 0 || iv ≥ es.size then M.trap .oob
        else pure es[iv.toNat]!
      | _ => M.trap (.stuck "indexing a non-array value")
  | .sliceOfSlice x lo hi => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (base, off, len, cap) ← M.expectSlice (← evalExpr fuel c fr x)
      let lov ← match lo with
        | some e => M.expectInt (← evalExpr fuel c fr e)
        | none => pure 0
      let hiv ← match hi with
        | some e => M.expectInt (← evalExpr fuel c fr e)
        | none => pure (Int.ofNat len)
      if lov < 0 || lov > hiv || hiv > cap then M.trap .oob
      else
        pure (.slice base (off + lov.toNat) (hiv - lov).toNat
              (cap - lov.toNat))
  | .sliceOfArr basep n lo hi => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (obj, g, path) ← evalPlace fuel c fr basep
      let lov ← match lo with
        | some e => M.expectInt (← evalExpr fuel c fr e)
        | none => pure 0
      let hiv ← match hi with
        | some e => M.expectInt (← evalExpr fuel c fr e)
        | none => pure (Int.ofNat n)
      if lov < 0 || lov > hiv || hiv > n then M.trap .oob
      else
        pure (.slice (some (obj, g, path)) lov.toNat (hiv - lov).toNat
              (n - lov.toNat))
  | .sliceOfStr x lo hi => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let s ← M.expectStr (← evalExpr fuel c fr x)
      let lov ← match lo with
        | some e => M.expectInt (← evalExpr fuel c fr e)
        | none => pure 0
      let hiv ← match hi with
        | some e => M.expectInt (← evalExpr fuel c fr e)
        | none => pure (Int.ofNat s.size)
      if lov < 0 || lov > hiv || hiv > s.size then M.trap .oob
      else
        pure (.s (s.extract lov.toNat hiv.toNat))
  | .callFn idx args => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      match ← evalCall fuel c fr idx args with
      | [v] => pure v
      | _ => M.trap (.stuck "call value arity")
  | .addrOf p => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (obj, g, path) ← evalPlace fuel c fr p
      pure (.ptr obj g path)
  | .arith op w x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectInt (← evalExpr fuel c fr x)
      let b ← M.expectInt (← evalExpr fuel c fr y)
      match applyArith op w a b with
      | .ok v => pure (.i v)
      | .error t => M.trap t
  | .shift left w x count => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectInt (← evalExpr fuel c fr x)
      let n ← M.expectInt (← evalExpr fuel c fr count)
      match applyShift left w a n with
      | .ok v => pure (.i v)
      | .error t => M.trap t
  | .icmp op x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectInt (← evalExpr fuel c fr x)
      let b ← M.expectInt (← evalExpr fuel c fr y)
      pure (.b (applyCmp op (compare a b)))
  | .scmp op x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectStr (← evalExpr fuel c fr x)
      let b ← M.expectStr (← evalExpr fuel c fr y)
      pure (.b (applyCmp op (strCompare a b)))
  | .beq ne x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectBool (← evalExpr fuel c fr x)
      let b ← M.expectBool (← evalExpr fuel c fr y)
      pure (.b (if ne then a != b else a == b))
  | .deepEq ne x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← evalExpr fuel c fr x
      let b ← evalExpr fuel c fr y
      match valEq fuel a b with
      | .ok r => pure (.b (if ne then !r else r))
      | .error t => M.trap t
  | .nilChk ne x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (base, _, _, _) ← M.expectSlice (← evalExpr fuel c fr x)
      let isNil := base.isNone
      pure (.b (if ne then !isNil else isNil))
  | .land x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectBool (← evalExpr fuel c fr x)
      if a then do
        let b ← M.expectBool (← evalExpr fuel c fr y)
        pure (.b b)
      else pure (.b false)
  | .lor x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectBool (← evalExpr fuel c fr x)
      if a then pure (.b true)
      else do
        let b ← M.expectBool (← evalExpr fuel c fr y)
        pure (.b b)
  | .lnot x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectBool (← evalExpr fuel c fr x)
      pure (.b (!a))
  | .negI w x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectInt (← evalExpr fuel c fr x)
      pure (.i (w.wrap (-a)))
  | .bnotI w x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectInt (← evalExpr fuel c fr x)
      pure (.i (w.wrap (-a - 1)))
  | .convI w x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectInt (← evalExpr fuel c fr x)
      pure (.i (w.wrap a))
  | .strToBytes x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let s ← M.expectStr (← evalExpr fuel c fr x)
      doStrToBytes s
  | .bytesToStr x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (base, off, len, _) ← M.expectSlice (← evalExpr fuel c fr x)
      let vs ← M.readElems base off len
      doBytesToStr vs
  | .lenSlice x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (_, _, len, _) ← M.expectSlice (← evalExpr fuel c fr x)
      pure (.i len)
  | .lenStr x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let s ← M.expectStr (← evalExpr fuel c fr x)
      pure (.i s.size)
  | .lenArr x n => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let _ ← evalExpr fuel c fr x
      pure (.i n)
  | .capSlice x => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (_, _, _, cap) ← M.expectSlice (← evalExpr fuel c fr x)
      pure (.i cap)
  | .appendE s elems elemTy => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let sv ← evalExpr fuel c fr s
      let adds ← evalExprs fuel c fr elems
      doAppend c sv adds.toArray elemTy
  | .appendSpread s src srcIsStr elemTy => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let sv ← evalExpr fuel c fr s
      let adds ← M.gatherSrc (← evalExpr fuel c fr src) srcIsStr
      doAppend c sv adds elemTy
  | .makeE elemTy len capE => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let n ← M.expectInt (← evalExpr fuel c fr len)
      let cp ← match capE with
        | some e => M.expectInt (← evalExpr fuel c fr e)
        | none => pure n
      doMake c elemTy n cp
  | .copyE dst src srcIsStr => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (dbase, doff, dlen, _) ← M.expectSlice (← evalExpr fuel c fr dst)
      let svals ← M.gatherSrc (← evalExpr fuel c fr src) srcIsStr
      let n := Nat.min dlen svals.size
      M.writeElems dbase doff 0 (svals.extract 0 n)
      pure (.i n)
  | .minE x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectInt (← evalExpr fuel c fr x)
      let b ← M.expectInt (← evalExpr fuel c fr y)
      pure (.i (min a b))
  | .maxE x y => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let a ← M.expectInt (← evalExpr fuel c fr x)
      let b ← M.expectInt (← evalExpr fuel c fr y)
      pure (.i (max a b))
  | .mkStruct fields => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let vs ← evalExprs fuel c fr fields
      pure (.strukt vs.toArray)
  | .mkArr elems pad elemTy => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let vs ← evalExprs fuel c fr elems
      pure (.arr (vs.toArray ++ Array.replicate pad (zeroVal c.structs elemTy)))
  | .mkSliceLit elemTy elems => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let vs ← evalExprs fuel c fr elems
      doSliceLit c elemTy vs.toArray

/-- Run one function on already-evaluated arguments: build the
frame, execute, free the frame, map the flow to results. The
harness calls enter here too. -/
def runFn (fuel : Nat) (c : Ctx) (fn : TFunc) (avs : List Val) :
    M (List Val) := do
  match fuel with
  | 0 => M.trap .fuel
  | fuel + 1 => do
    M.enterFn
    let mut frame : Frame := Array.replicate fn.nslots 0
    let mut i := 0
    for v in avs do
      let (cell, _) ← M.alloc v
      frame := frame.set! i cell
      i := i + 1
    let (flow, fr') ← execStmts fuel c frame fn.body
    M.freeFrame fr'
    M.exitFn
    match flow with
    | .retv vs => pure vs
    | .normal =>
      if fn.results.isEmpty then pure []
      else M.trap (.stuck "missing return")
    | _ => M.trap (.stuck "loose break")

/-- Call a function: arguments are evaluated by the caller. -/
def evalCall (fuel : Nat) (c : Ctx) (fr : Frame) (idx : Nat)
    (args : List TExpr) : M (List Val) := do
  match fuel with
  | 0 => M.trap .fuel
  | fuel + 1 => do
    let some fn := c.funcs[idx]? | M.trap (.stuck "bad function index")
    let avs ← evalExprs fuel c fr args
    runFn fuel c fn avs

def execStmts (fuel : Nat) (c : Ctx) (fr : Frame) :
    List TStmt → M (Flow × Frame)
  | [] => pure (.normal, fr)
  | s :: rest => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      M.tickStmt
      let (f, fr') ← execStmt fuel c fr s
      match f with
      | .normal => execStmts fuel c fr' rest
      | _ => pure (f, fr')

/-- One iteration chain of a for loop. -/
def execForLoop (fuel : Nat) (c : Ctx) (fr : Frame)
    (cond : Option TExpr) (post : Option TStmt) (body : List TStmt) :
    M Flow := do
  match fuel with
  | 0 => M.trap .fuel
  | fuel + 1 => do
    M.tickLoop
    let go ← match cond with
      | some e => M.expectBool (← evalExpr fuel c fr e)
      | none => pure true
    if !go then pure .normal
    else do
      let (f, _) ← execStmts fuel c fr body
      match f with
      | .brk => pure .normal
      | .retv vs => pure (.retv vs)
      | _ => do
        match post with
        | some p => do
          let _ ← execStmt fuel c fr p
          execForLoop fuel c fr cond post body
        | none => execForLoop fuel c fr cond post body

/-- Range loops over an index sequence: `next` yields the element
for the value variable, reading the buffer at each step. -/
def execRangeLoop (fuel : Nat) (c : Ctx) (fr : Frame) (k n : Nat)
    (iCell : Option Nat) (vCell : Option Nat)
    (elemAt : Nat → M Val) (body : List TStmt) : M Flow := do
  match fuel with
  | 0 => M.trap .fuel
  | fuel + 1 => do
    M.tickLoop
    if k ≥ n then pure .normal
    else do
      match iCell with
      | some cell => M.writeCell cell (.i k)
      | none => pure ()
      match vCell with
      | some cell => do
        M.writeCell cell (← elemAt k)
      | none => pure ()
      let (f, _) ← execStmts fuel c fr body
      match f with
      | .brk => pure .normal
      | .retv vs => pure (.retv vs)
      | _ => execRangeLoop fuel c fr (k + 1) n iCell vCell elemAt body

def execStmt (fuel : Nat) (c : Ctx) (fr : Frame) :
    TStmt → M (Flow × Frame)
  | .newVar slot init => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let v ← evalExpr fuel c fr init
      let (fr', _) ← M.rebindSlot fr slot v
      pure (.normal, fr')
  | .defineCall2 s1 s2 call => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let vs ← evalCallExpr fuel c fr call
      let [v1, v2] := vs | M.trap (.stuck "two-value define arity")
      let mut fr' := fr
      match s1 with
      | some slot => do
        fr' := (← M.rebindSlot fr' slot v1).1
      | none => pure ()
      match s2 with
      | some slot => do
        fr' := (← M.rebindSlot fr' slot v2).1
      | none => pure ()
      pure (.normal, fr')
  | .assign1 lhs value => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      match lhs with
      | some p => do
        let (obj, g, path) ← evalPlace fuel c fr p
        let v ← evalExpr fuel c fr value
        M.writeLoc obj g path v
        pure (.normal, fr)
      | none => do
        let _ ← evalExpr fuel c fr value
        pure (.normal, fr)
  | .assignCall2 l1 l2 call => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let loc1 ← match l1 with
        | some p => do pure (some (← evalPlace fuel c fr p))
        | none => pure none
      let loc2 ← match l2 with
        | some p => do pure (some (← evalPlace fuel c fr p))
        | none => pure none
      let vs ← evalCallExpr fuel c fr call
      let [v1, v2] := vs | M.trap (.stuck "two-value assign arity")
      match loc1 with
      | some (obj, g, path) => M.writeLoc obj g path v1
      | none => pure ()
      match loc2 with
      | some (obj, g, path) => M.writeLoc obj g path v2
      | none => pure ()
      pure (.normal, fr)
  | .opAssignA op w lhs value => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (obj, g, path) ← evalPlace fuel c fr lhs
      let b ← M.expectInt (← evalExpr fuel c fr value)
      let a ← M.expectInt (← M.readLoc obj g path)
      match applyArith op w a b with
      | .ok v => do
        M.writeLoc obj g path (.i v)
        pure (.normal, fr)
      | .error t => M.trap t
  | .opAssignSh left w lhs count => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (obj, g, path) ← evalPlace fuel c fr lhs
      let n ← M.expectInt (← evalExpr fuel c fr count)
      let a ← M.expectInt (← M.readLoc obj g path)
      match applyShift left w a n with
      | .ok v => do
        M.writeLoc obj g path (.i v)
        pure (.normal, fr)
      | .error t => M.trap t
  | .incdec inc w lhs => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (obj, g, path) ← evalPlace fuel c fr lhs
      let a ← M.expectInt (← M.readLoc obj g path)
      let v := w.wrap (if inc then a + 1 else a - 1)
      M.writeLoc obj g path (.i v)
      pure (.normal, fr)
  | .ifS cond thn els => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let b ← M.expectBool (← evalExpr fuel c fr cond)
      let (f, _) ← execStmts fuel c fr (if b then thn else els)
      pure (f, fr)
  | .forS ini cond post body => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let fr1 ← match ini with
        | some s => do pure (← execStmt fuel c fr s).2
        | none => pure fr
      let f ← execForLoop fuel c fr1 cond post body
      pure (f, fr)
  | .rangeSlice iSlot vSlot over body => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (base, off, len, _) ← M.expectSlice (← evalExpr fuel c fr over)
      let (fr1, iCell) ← allocLoopCell fr iSlot
      let (fr2, vCell) ← allocLoopCell fr1 vSlot
      let f ← execRangeLoop fuel c fr2 0 len iCell vCell
        (fun k => M.readElem base off k) body
      pure (f, fr)
  | .rangeArr iSlot vSlot over body => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let ov ← evalExpr fuel c fr over
      let es ← match ov with
        | .arr es => pure es
        | _ => M.trap (.stuck "range over a non-array")
      let (fr1, iCell) ← allocLoopCell fr iSlot
      let (fr2, vCell) ← allocLoopCell fr1 vSlot
      let f ← execRangeLoop fuel c fr2 0 es.size iCell vCell
        (fun k => pure es[k]!) body
      pure (f, fr)
  | .rangeInt iSlot over body => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let n ← M.expectInt (← evalExpr fuel c fr over)
      let bound := if n ≤ 0 then 0 else n.toNat
      let (fr1, iCell) ← allocLoopCell fr iSlot
      let f ← execRangeLoop fuel c fr1 0 bound iCell none
        (fun _ => M.trap (.stuck "no range value")) body
      pure (f, fr)
  | .switchS tag cases dflt => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let tv ← evalExpr fuel c fr tag
      let body ← findCase fuel c fr tv cases dflt
      let (f, _) ← execStmts fuel c fr body
      pure (f, fr)
  | .breakS => pure (.brk, fr)
  | .continueS => pure (.cont, fr)
  | .ret values => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let vs ← evalExprs fuel c fr values
      pure (.retv vs, fr)
  | .retCall call => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let vs ← evalCallExpr fuel c fr call
      pure (.retv vs, fr)
  | .exprStmt e => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      match e with
      | .callFn idx args => do
        let _ ← evalCall fuel c fr idx args
        pure (.normal, fr)
      | _ => do
        let _ ← evalExpr fuel c fr e
        pure (.normal, fr)
  | .blockS body => do
    match fuel with
    | 0 => M.trap .fuel
    | fuel + 1 => do
      let (f, _) ← execStmts fuel c fr body
      pure (f, fr)

def anyCaseMatch (fuel : Nat) (c : Ctx) (fr : Frame) (tv : Val)
    (vals : List TExpr) : M Bool := do
  match fuel with
  | 0 => M.trap .fuel
  | fuel + 1 => do
    match vals with
    | [] => pure false
    | ve :: rest => do
      let v ← evalExpr fuel c fr ve
      match valEq fuel tv v with
      | .ok true => pure true
      | .ok false => anyCaseMatch fuel c fr tv rest
      | .error t => M.trap t

def findCase (fuel : Nat) (c : Ctx) (fr : Frame) (tv : Val)
    (cases : List (List TExpr × List TStmt)) (dflt : List TStmt) :
    M (List TStmt) := do
  match fuel with
  | 0 => M.trap .fuel
  | fuel + 1 => do
    match cases with
    | [] => pure dflt
    | (vals, body) :: rest => do
      if ← anyCaseMatch fuel c fr tv vals then pure body
      else findCase fuel c fr tv rest dflt

def evalCallExpr (fuel : Nat) (c : Ctx) (fr : Frame) (e : TExpr) :
    M (List Val) := do
  match fuel with
  | 0 => M.trap .fuel
  | fuel + 1 => do
    match e with
    | .callFn idx args => evalCall fuel c fr idx args
    | _ => M.trap (.stuck "expected a call")

end

end Vego
