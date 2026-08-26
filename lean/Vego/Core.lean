/-
The typed core of Vego.

The elaborator in Elab.lean turns the raw JSON syntax tree into
this form. Every operation carries the width it wraps at, every
name is resolved to an index, and every implicit zero fill is
explicit. The interpreter in Interp.lean runs this form directly,
so it never has to guess a type at run time.
-/

import Vego.Ast

namespace Vego

/-- Semantic types. Struct and pointer types refer to the struct
table by position. Array lengths are resolved constants. -/
inductive VTy where
  | bool
  | int (w : IW)
  | str
  | slice (elem : VTy)
  | arr (n : Nat) (elem : VTy)
  | strukt (idx : Nat)
  | ptr (idx : Nat)
  deriving Repr, BEq, Inhabited

/-- Arithmetic and bitwise operators that wrap at a width. -/
inductive ArithOp where
  | add | sub | mul | quo | rem | band | bor | bxor | andNot
  deriving Repr, BEq

/-- Ordering comparisons. Equality tests get their own typed
constructors. -/
inductive CmpOp where
  | eq | ne | lt | le | gt | ge
  deriving Repr, BEq

mutual

/-- A place: a memory location an assignment or a borrow can hit.
`indexSliceP` evaluates a slice-valued expression to a header and
addresses one element of the shared buffer. -/
inductive TPlace where
  | localP (slot : Nat)
  | fieldP (base : TPlace) (i : Nat) (viaPtr : Bool)
  | indexArrP (base : TPlace) (n : Nat) (i : TExpr)
  | indexSliceP (sliceVal : TExpr) (i : TExpr)
  deriving Repr

/-- Typed expressions. Literal integers are already wrapped into
their width. -/
inductive TExpr where
  | litInt (v : Int)
  | litBool (b : Bool)
  | litStr (bytes : ByteArray)
  | zeroOf (ty : VTy)
  | globalGet (idx : Nat)
  | placeGet (p : TPlace)
  | fieldGet (x : TExpr) (i : Nat)
  | indexSliceGet (x : TExpr) (i : TExpr)
  | indexStrGet (x : TExpr) (i : TExpr)
  | indexArrVal (x : TExpr) (i : TExpr)
  | sliceOfSlice (x : TExpr) (lo : Option TExpr) (hi : Option TExpr)
  | sliceOfArr (base : TPlace) (n : Nat) (lo : Option TExpr)
               (hi : Option TExpr)
  | sliceOfStr (x : TExpr) (lo : Option TExpr) (hi : Option TExpr)
  | callFn (idx : Nat) (args : List TExpr)
  | addrOf (p : TPlace)
  | arith (op : ArithOp) (w : IW) (x : TExpr) (y : TExpr)
  | shift (left : Bool) (w : IW) (x : TExpr) (count : TExpr)
  | icmp (op : CmpOp) (x : TExpr) (y : TExpr)
  | scmp (op : CmpOp) (x : TExpr) (y : TExpr)
  | beq (ne : Bool) (x : TExpr) (y : TExpr)
  | deepEq (ne : Bool) (x : TExpr) (y : TExpr)
  | nilChk (ne : Bool) (x : TExpr)
  | land (x : TExpr) (y : TExpr)
  | lor (x : TExpr) (y : TExpr)
  | lnot (x : TExpr)
  | negI (w : IW) (x : TExpr)
  | bnotI (w : IW) (x : TExpr)
  | convI (w : IW) (x : TExpr)
  | strToBytes (x : TExpr)
  | bytesToStr (x : TExpr)
  | lenSlice (x : TExpr)
  | lenStr (x : TExpr)
  | lenArr (x : TExpr) (n : Nat)
  | capSlice (x : TExpr)
  | appendE (s : TExpr) (elems : List TExpr) (elemTy : VTy)
  | appendSpread (s : TExpr) (src : TExpr) (srcIsStr : Bool) (elemTy : VTy)
  | makeE (elemTy : VTy) (len : TExpr) (capE : Option TExpr)
  | copyE (dst : TExpr) (src : TExpr) (srcIsStr : Bool)
  | minE (x : TExpr) (y : TExpr)
  | maxE (x : TExpr) (y : TExpr)
  | mkStruct (fields : List TExpr)
  | mkArr (elems : List TExpr) (pad : Nat) (elemTy : VTy)
  | mkSliceLit (elems : List TExpr)
  deriving Repr

end

/-- Typed statements. Slots index the function frame; `none` in a
slot or place position is the blank identifier. -/
inductive TStmt where
  | newVar (slot : Nat) (init : TExpr)
  | defineCall2 (s1 : Option Nat) (s2 : Option Nat) (call : TExpr)
  | assign1 (lhs : Option TPlace) (value : TExpr)
  | assignCall2 (l1 : Option TPlace) (l2 : Option TPlace) (call : TExpr)
  | opAssignA (op : ArithOp) (w : IW) (lhs : TPlace) (value : TExpr)
  | opAssignSh (left : Bool) (w : IW) (lhs : TPlace) (count : TExpr)
  | incdec (inc : Bool) (w : IW) (lhs : TPlace)
  | ifS (cond : TExpr) (thn : List TStmt) (els : List TStmt)
  | forS (ini : Option TStmt) (cond : Option TExpr) (post : Option TStmt)
         (body : List TStmt)
  | rangeSlice (iSlot : Option Nat) (vSlot : Option Nat) (over : TExpr)
               (body : List TStmt)
  | rangeArr (iSlot : Option Nat) (vSlot : Option Nat) (over : TExpr)
             (body : List TStmt)
  | rangeInt (iSlot : Option Nat) (over : TExpr) (body : List TStmt)
  | switchS (tag : TExpr) (cases : List (List TExpr × List TStmt))
            (dflt : List TStmt)
  | breakS
  | continueS
  | ret (values : List TExpr)
  | retCall (call : TExpr)
  | exprStmt (e : TExpr)
  | blockS (body : List TStmt)
  deriving Repr

/-- Byte-order comparison of strings, shared by the interpreter and
by the elaborator's constant folding. -/
def byteCompare (a b : ByteArray) (i : Nat) (fuel : Nat) : Ordering :=
  match fuel with
  | 0 => .eq
  | fuel + 1 =>
    if h : i < a.size then
      if h2 : i < b.size then
        let x := a[i]
        let y := b[i]
        if x < y then .lt
        else if x > y then .gt
        else byteCompare a b (i + 1) fuel
      else .gt
    else if i < b.size then .lt
    else .eq

def strCompare (a b : ByteArray) : Ordering :=
  byteCompare a b 0 (max a.size b.size + 1)

def applyCmp (op : CmpOp) (o : Ordering) : Bool :=
  match op with
  | .eq => o == .eq
  | .ne => o != .eq
  | .lt => o == .lt
  | .le => o != .gt
  | .gt => o == .gt
  | .ge => o != .lt

structure TFunc where
  name : String
  paramTys : List VTy
  results : List VTy
  body : List TStmt
  nslots : Nat
  deriving Repr

/-- A checked program: struct layouts, evaluated globals come later
at run time from `globalInits`, and functions by index. -/
structure TProgram where
  structNames : Array String
  structFields : Array (Array String)
  structs : Array (Array VTy)
  globalInits : Array TExpr
  funcs : Array TFunc
  deriving Repr

end Vego
