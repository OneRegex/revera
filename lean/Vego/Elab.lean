/-
Elaboration of the raw syntax tree into the typed core.

This pass replays the typing knowledge that the Go compiler and the vegoc checker guarantee.
It resolves every name and gives every operation its wrapping width.
It folds untyped constant arithmetic exactly the way Go does, and it inserts the implicit zero values.
A program that elaborates is well formed, and that success is one of the machine-checked theorems.
-/

import Vego.Core

namespace Vego

abbrev E := Except String

/-- Width bounds, as precomputed constants: the interpreter reads them on every wrap. -/
def IW.lo : IW → Int
  | .u8 | .u16 | .u32 | .u64 => 0
  | .i32 => -2147483648
  | .i64 => -9223372036854775808

def IW.hi : IW → Int
  | .u8 => 255
  | .u16 => 65535
  | .u32 => 4294967295
  | .u64 => 18446744073709551615
  | .i32 => 2147483647
  | .i64 => 9223372036854775807

def IW.bits : IW → Nat
  | .u8 => 8 | .u16 => 16 | .u32 => 32 | .u64 => 64
  | .i32 => 32 | .i64 => 64

def IW.signed : IW → Bool
  | .i32 | .i64 => true
  | _ => false

def IW.fits (w : IW) (v : Int) : Bool :=
  w.lo ≤ v && v ≤ w.hi

/-- The value count of a width, as a precomputed constant: the interpreter wraps on every operation, so this must not recompute a power. -/
def IW.modulus : IW → Int
  | .u8 => 256
  | .u16 => 65536
  | .u32 => 4294967296
  | .u64 => 18446744073709551616
  | .i32 => 4294967296
  | .i64 => 18446744073709551616

/-- Two's-complement wrap of an integer into a width. -/
def IW.wrap (w : IW) (v : Int) : Int :=
  let m := w.modulus
  let u := v % m
  let u := if u < 0 then u + m else u
  if w.signed && u > w.hi then u - m else u

/--
Constant values during elaboration.
An integer carries its width when a conversion or a typed constant fixed it.
-/
inductive CE where
  | i (v : Int) (w : Option IW)
  | b (v : Bool)
  | s (v : ByteArray)
  deriving Repr

structure StructInfo where
  idx : Nat
  fieldNames : List String
  fieldTys : Array VTy

structure FnSig where
  idx : Nat
  params : List VTy
  results : List VTy

structure Genv where
  structs : List (String × StructInfo)
  consts : List (String × CE)
  globals : List (String × Nat × VTy)
  fns : List (String × FnSig)

private def lookup? (xs : List (String × α)) (n : String) : Option α :=
  xs.lookup n

private def scalarIW? : String → Option IW
  | "uint8" => some .u8
  | "uint16" => some .u16
  | "uint32" => some .u32
  | "uint64" => some .u64
  | "int32" => some .i32
  | "int64" => some .i64
  | "int" => some .i64
  | _ => none

/-- Bitwise operations on unbounded integers, two's complement with infinite sign extension. -/
def intAnd : Int → Int → Int
  | .ofNat x, .ofNat y => .ofNat (x &&& y)
  | .ofNat x, .negSucc y => .ofNat (x ^^^ (x &&& y))
  | .negSucc x, .ofNat y => .ofNat (y ^^^ (x &&& y))
  | .negSucc x, .negSucc y => .negSucc (x ||| y)

def intOr : Int → Int → Int
  | .ofNat x, .ofNat y => .ofNat (x ||| y)
  | .ofNat x, .negSucc y => .negSucc (y ^^^ (x &&& y))
  | .negSucc x, .ofNat y => .negSucc (x ^^^ (x &&& y))
  | .negSucc x, .negSucc y => .negSucc (x &&& y)

def intXor : Int → Int → Int
  | .ofNat x, .ofNat y => .ofNat (x ^^^ y)
  | .ofNat x, .negSucc y => .negSucc (x ^^^ y)
  | .negSucc x, .ofNat y => .negSucc (x ^^^ y)
  | .negSucc x, .negSucc y => .ofNat (x ^^^ y)

/-- Exact constant arithmetic, Go's untyped rules: no wrapping, truncated division. -/
private def foldArith (op : BinOp) (a b : Int) : E Int :=
  match op with
  | .add => pure (a + b)
  | .sub => pure (a - b)
  | .mul => pure (a * b)
  | .quo =>
    if b == 0 then throw "constant division by zero" else pure (Int.tdiv a b)
  | .rem =>
    if b == 0 then throw "constant division by zero" else pure (Int.tmod a b)
  | .band => pure (intAnd a b)
  | .bor => pure (intOr a b)
  | .bxor => pure (intXor a b)
  | .andNot => pure (intAnd a (intXor (-1) b))
  | .shl =>
    if b < 0 || b > 512 then throw "bad constant shift count"
    else pure (a * 2 ^ b.toNat)
  | .shr =>
    if b < 0 || b > 512 then throw "bad constant shift count"
    else pure (Int.fdiv a (2 ^ b.toNat))
  | _ => throw "not an arithmetic operator"

private def cmpInt (op : BinOp) (a b : Int) : E Bool :=
  match op with
  | .eq => pure (a == b) | .ne => pure (a != b)
  | .lt => pure (a < b) | .le => pure (a ≤ b)
  | .gt => pure (a > b) | .ge => pure (a ≥ b)
  | _ => throw "not a comparison"

private def mergeW (a b : Option IW) : E (Option IW) :=
  match a, b with
  | none, w => pure w
  | w, none => pure w
  | some x, some y =>
    if x == y then pure (some x) else throw "mismatched constant widths"

/--
Evaluate a constant expression: literals, named constants, and the scalar operators.
Used for constant declarations, array lengths, and global initializers.
-/
private def constEval (consts : List (String × CE)) : Expr → E CE
  | .intLit v => pure (.i v none)
  | .strLit s => pure (.s s)
  | .boolLit b => pure (.b b)
  | .ident n =>
    match lookup? consts n with
    | some c => pure c
    | none => throw s!"not a constant: {n}"
  | .unary .neg x => do
    match ← constEval consts x with
    | .i v w => pure (.i (-v) w)
    | _ => throw "negation of a non-integer constant"
  | .unary .bnot x => do
    match ← constEval consts x with
    | .i v w => pure (.i (-v - 1) w)
    | _ => throw "complement of a non-integer constant"
  | .unary .lnot x => do
    match ← constEval consts x with
    | .b v => pure (.b (!v))
    | _ => throw "logical not of a non-bool constant"
  | .unary .addr _ => throw "address is not constant"
  | .binary op x y => do
    match ← constEval consts x, ← constEval consts y with
    | .i a wa, .i b wb =>
      match op with
      | .eq | .ne | .lt | .le | .gt | .ge => pure (.b (← cmpInt op a b))
      | .shl | .shr => pure (.i (← foldArith op a b) wa)
      | _ => pure (.i (← foldArith op a b) (← mergeW wa wb))
    | .b a, .b b =>
      match op with
      | .land => pure (.b (a && b))
      | .lor => pure (.b (a || b))
      | .eq => pure (.b (a == b))
      | .ne => pure (.b (a != b))
      | _ => throw "bad boolean constant operator"
    | _, _ => throw "mixed constant operands"
  | .conv (.named t) x => do
    match scalarIW? t, ← constEval consts x with
    | some w, .i v _ =>
      if w.fits v then pure (.i v (some w))
      else throw s!"constant {v} overflows {t}"
    | _, _ => throw "bad constant conversion"
  | _ => throw "not a constant expression"

/-- Turn a syntactic type into a semantic one. -/
private def tyToVTy (g : Genv) : Ty → E VTy
  | .named "bool" => pure .bool
  | .named "string" => pure .str
  | .named n =>
    match scalarIW? n with
    | some w => pure (.int w)
    | none => throw s!"unknown type {n}"
  | .slice e => do pure (.slice (← tyToVTy g e))
  | .arr len e => do
    match ← constEval g.consts len with
    | .i v _ =>
      if v < 0 then throw "negative array length"
      else pure (.arr v.toNat (← tyToVTy g e))
    | _ => throw "array length is not an integer"
  | .structRef n =>
    match lookup? g.structs n with
    | some si => pure (.strukt si.idx)
    | none => throw s!"unknown struct {n}"
  | .ptr n =>
    match lookup? g.structs n with
    | some si => pure (.ptr si.idx)
    | none => throw s!"unknown struct {n}"

/-- Local scopes: name to slot and type, innermost first. -/
structure Lctx where
  scopes : List (List (String × Nat × VTy))
  nslots : Nat
  loopDepth : Nat
  results : List VTy

private def Lctx.find? (c : Lctx) (n : String) : Option (Nat × VTy) :=
  c.scopes.findSome? (fun sc => lookup? sc n)

private def Lctx.bind (c : Lctx) (n : String) (ty : VTy) :
    Lctx × Nat :=
  let slot := c.nslots
  match c.scopes with
  | [] => ({ c with scopes := [[(n, slot, ty)]], nslots := slot + 1 }, slot)
  | sc :: rest =>
    ({ c with scopes := ((n, slot, ty) :: sc) :: rest, nslots := slot + 1 },
     slot)

/-- Elaboration result: a typed expression or a constant that still adapts to its context. -/
inductive ER where
  | typed (e : TExpr) (ty : VTy)
  | ci (v : Int)
  | cb (v : Bool)
  | cs (v : ByteArray)
  | cnil

/--
Force a result to a concrete type.
Untyped integer constants must be representable, as in Go.
-/
private def coerce (r : ER) (want : Option VTy) : E (TExpr × VTy) :=
  match r, want with
  | .typed e t, none => pure (e, t)
  | .typed e t, some w =>
    if t == w then pure (e, t) else throw "type mismatch"
  | .ci v, none =>
    if IW.i64.fits v then pure (.litInt v, .int .i64)
    else throw "integer constant overflows int"
  | .ci v, some (.int w) =>
    if w.fits v then pure (.litInt v, .int w)
    else throw s!"integer constant {v} overflows its context"
  | .ci _, some _ => throw "integer constant in a non-integer context"
  | .cb v, none => pure (.litBool v, .bool)
  | .cb v, some .bool => pure (.litBool v, .bool)
  | .cb _, some _ => throw "bool constant in a non-bool context"
  | .cs v, none => pure (.litStr v, .str)
  | .cs v, some .str => pure (.litStr v, .str)
  | .cs _, some _ => throw "string constant in a non-string context"
  | .cnil, some (.slice e) => pure (.zeroOf (.slice e), .slice e)
  | .cnil, _ => throw "nil outside a slice context"

/-- An operand of any integer type, for indexes, shift counts, and make sizes. -/
private def coerceAnyInt (r : ER) : E TExpr :=
  match r with
  | .typed e (.int _) => pure e
  | .ci v =>
    if IW.i64.fits v || IW.u64.fits v then pure (.litInt v)
    else throw "integer constant out of range"
  | _ => throw "expected an integer"

private def binToArith : BinOp → Option ArithOp
  | .add => some .add | .sub => some .sub | .mul => some .mul
  | .quo => some .quo | .rem => some .rem
  | .band => some .band | .bor => some .bor | .bxor => some .bxor
  | .andNot => some .andNot
  | _ => none

private def binToCmp : BinOp → Option CmpOp
  | .eq => some .eq | .ne => some .ne
  | .lt => some .lt | .le => some .le
  | .gt => some .gt | .ge => some .ge
  | _ => none

mutual

/--
Elaborate an expression.
`want` is the context type, and untyped constants adapt to it.
-/
private partial def elabExpr (g : Genv) (c : Lctx) (want : Option VTy)
    (e : Expr) : E ER := do
  match e with
  | .intLit v => pure (.ci v)
  | .strLit s => pure (.cs s)
  | .boolLit b => pure (.cb b)
  | .ident "nil" => pure .cnil
  | .ident n =>
    match c.find? n with
    | some (slot, ty) => pure (.typed (.placeGet (.localP slot)) ty)
    | none =>
      match lookup? g.consts n with
      | some (.i v none) => pure (.ci v)
      | some (.i v (some w)) => pure (.typed (.litInt v) (.int w))
      | some (.b v) => pure (.cb v)
      | some (.s v) => pure (.cs v)
      | none =>
        match lookup? g.globals n with
        | some (idx, ty) => pure (.typed (.globalGet idx) ty)
        | none => throw s!"unknown identifier {n}"
  | .field x f => do
    let (tx, ty) ← coerce (← elabExpr g c none x) none
    let si ← match ty with
      | .strukt i | .ptr i => structAt g i
      | _ => throw "field access on a non-struct"
    let some i := si.fieldNames.idxOf? f
      | throw s!"unknown field {f}"
    pure (.typed (.fieldGet tx i) si.fieldTys[i]!)
  | .index x i => do
    let ti ← coerceAnyInt (← elabExpr g c none i)
    match ← elabExpr g c none x with
    | .typed tx (.slice e) => pure (.typed (.indexSliceGet tx ti) e)
    | .typed tx .str => pure (.typed (.indexStrGet tx ti) (.int .u8))
    | .cs s => pure (.typed (.indexStrGet (.litStr s) ti) (.int .u8))
    | .typed tx (.arr n e) =>
      match elabPlace g c x with
      | .ok (p, _) => pure (.typed (.placeGet (.indexArrP p n ti)) e)
      | .error _ => pure (.typed (.indexArrVal tx ti) e)
    | _ => throw "indexing a non-indexable value"
  | .sliceE x lo hi => do
    let tlo ← lo.mapM (fun e => do coerceAnyInt (← elabExpr g c none e))
    let thi ← hi.mapM (fun e => do coerceAnyInt (← elabExpr g c none e))
    match ← elabExpr g c none x with
    | .typed tx (.slice e) => pure (.typed (.sliceOfSlice tx tlo thi) (.slice e))
    | .typed tx .str => pure (.typed (.sliceOfStr tx tlo thi) .str)
    | .cs s => pure (.typed (.sliceOfStr (.litStr s) tlo thi) .str)
    | .typed _ (.arr n e) => do
      let (p, _) ← elabPlace g c x
      pure (.typed (.sliceOfArr p n tlo thi) (.slice e))
    | _ => throw "slicing a non-sliceable value"
  | .call fn args => do
    let (te, results) ← elabCall g c fn args
    match results with
    | [ty] => pure (.typed te ty)
    | _ => throw s!"call to {fn} used as a value needs one result"
  | .builtin fn args spread mty => elabBuiltin g c fn args spread mty
  | .conv ty x => do
    match ty with
    | .named t =>
      match scalarIW? t with
      | some w => do
        match ← elabExpr g c none x with
        | .ci v =>
          if w.fits v then pure (.typed (.litInt v) (.int w))
          else throw s!"constant {v} overflows {t}"
        | .typed tx (.int _) => pure (.typed (.convI w tx) (.int w))
        | _ => throw "bad scalar conversion operand"
      | none =>
        if t == "string" then do
          let (tx, ty) ← coerce (← elabExpr g c none x) none
          match ty with
          | .slice (.int .u8) => pure (.typed (.bytesToStr tx) .str)
          | _ => throw "string conversion needs a byte slice"
        else throw s!"bad conversion target {t}"
    | .slice (.named "uint8") => do
      let (tx, _) ← coerce (← elabExpr g c none x) (some .str)
      pure (.typed (.strToBytes tx) (.slice (.int .u8)))
    | _ => throw "unsupported conversion target"
  | .unary .neg x => do
    match ← elabExpr g c want x with
    | .ci v => pure (.ci (-v))
    | .typed tx (.int w) => pure (.typed (.negI w tx) (.int w))
    | _ => throw "negation of a non-integer"
  | .unary .bnot x => do
    match ← elabExpr g c want x with
    | .ci v => pure (.ci (-v - 1))
    | .typed tx (.int w) => pure (.typed (.bnotI w tx) (.int w))
    | _ => throw "complement of a non-integer"
  | .unary .lnot x => do
    match ← elabExpr g c none x with
    | .cb v => pure (.cb (!v))
    | .typed tx .bool => pure (.typed (.lnot tx) .bool)
    | _ => throw "logical not of a non-bool"
  | .unary .addr x => do
    let (p, ty) ← elabPlace g c x
    match ty with
    | .strukt i => pure (.typed (.addrOf p) (.ptr i))
    | _ => throw "address of a non-struct place"
  | .binary op x y => elabBinary g c want op x y
  | .compositeS ty fields => do
    let vt ← tyToVTy g ty
    let .strukt i := vt | throw "keyed composite literal needs a struct type"
    let si ← structAt g i
    let mut inits : Array TExpr := #[]
    for fname in si.fieldNames, fty in si.fieldTys do
      match fields.find? (·.1 == fname) with
      | some (_, fe) =>
        let (tf, _) ← coerce (← elabExpr g c (some fty) fe) (some fty)
        inits := inits.push tf
      | none => inits := inits.push (.zeroOf fty)
    for (fname, _) in fields do
      if !si.fieldNames.contains fname then
        throw s!"unknown field {fname} in composite literal"
    pure (.typed (.mkStruct inits.toList) (.strukt i))
  | .compositeL ty elems => do
    match ← tyToVTy g ty with
    | .slice e => do
      let tes ← elems.mapM (fun x => do
        pure (← coerce (← elabExpr g c (some e) x) (some e)).1)
      pure (.typed (.mkSliceLit e tes) (.slice e))
    | .arr n e => do
      if elems.length > n then throw "too many array literal elements"
      let tes ← elems.mapM (fun x => do
        pure (← coerce (← elabExpr g c (some e) x) (some e)).1)
      pure (.typed (.mkArr tes (n - elems.length) e) (.arr n e))
    | _ => throw "positional composite literal needs a slice or array type"

private partial def structAt (g : Genv) (i : Nat) : E StructInfo :=
  match g.structs.find? (·.2.idx == i) with
  | some (_, si) => pure si
  | none => throw "bad struct index"

private partial def elabBinary (g : Genv) (c : Lctx) (want : Option VTy)
    (op : BinOp) (x y : Expr) : E ER := do
  match op with
  | .land | .lor => do
    let (tx, _) ← coerce (← elabExpr g c none x) (some .bool)
    let (ty', _) ← coerce (← elabExpr g c none y) (some .bool)
    match tx, ty' with
    | .litBool a, .litBool b =>
      pure (.cb (if op == .land then a && b else a || b))
    | _, _ =>
      pure (.typed (if op == .land then .land tx ty' else .lor tx ty') .bool)
  | .shl | .shr => do
    let count ← coerceAnyInt (← elabExpr g c none y)
    match ← elabExpr g c want x with
    | .ci v =>
      match count with
      | .litInt n => pure (.ci (← foldArith op v n))
      | _ => do
        let (tx, txty) ← coerce (.ci v) want
        let .int w := txty | throw "shift of a non-integer"
        pure (.typed (.shift (op == .shl) w tx count) txty)
    | .typed tx (.int w) =>
      pure (.typed (.shift (op == .shl) w tx count) (.int w))
    | _ => throw "shift of a non-integer"
  | _ =>
    match binToCmp op with
    | some cop => do
      let rx ← elabExpr g c none x
      let ry ← elabExpr g c none y
      match rx, ry with
      | .cnil, r | r, .cnil => do
        let isNe ← match cop with
          | .eq => pure false
          | .ne => pure true
          | _ => throw "ordered comparison with nil"
        let (te, ty) ← coerce r none
        let .slice _ := ty | throw "nil compared with a non-slice"
        pure (.typed (.nilChk isNe te) .bool)
      | .ci a, .ci b => pure (.cb (← cmpInt op a b))
      | .cb a, .cb b =>
        match cop with
        | .eq => pure (.cb (a == b))
        | .ne => pure (.cb (a != b))
        | _ => throw "ordered comparison of bools"
      | .cs a, .cs b => pure (.cb (applyCmp cop (strCompare a b)))
      | _, _ => do
          let (tx, tyx) ← coerce rx none
          let (ty', _) ← coerce ry (some tyx)
          match tyx with
          | .int _ => pure (.typed (.icmp cop tx ty') .bool)
          | .str => pure (.typed (.scmp cop tx ty') .bool)
          | .bool =>
            match cop with
            | .eq => pure (.typed (.beq false tx ty') .bool)
            | .ne => pure (.typed (.beq true tx ty') .bool)
            | _ => throw "ordered comparison of bools"
          | .strukt _ | .arr _ _ =>
            match cop with
            | .eq => pure (.typed (.deepEq false tx ty') .bool)
            | .ne => pure (.typed (.deepEq true tx ty') .bool)
            | _ => throw "ordered comparison of composites"
          | _ => throw "uncomparable operands"
    | none => do
      let some aop := binToArith op | throw "unknown operator"
      let rx ← elabExpr g c want x
      match rx with
      | .ci a => do
        match ← elabExpr g c want y with
        | .ci b => pure (.ci (← foldArith op a b))
        | .typed ty' (.int w) => do
          if !w.fits a then throw "constant overflows its context"
          pure (.typed (.arith aop w (.litInt a) ty') (.int w))
        | _ => throw "arithmetic on a non-integer"
      | .typed tx (.int w) => do
        let (ty', _) ← coerce (← elabExpr g c (some (.int w)) y)
          (some (.int w))
        pure (.typed (.arith aop w tx ty') (.int w))
      | _ => throw "arithmetic on a non-integer"

private partial def elabCall (g : Genv) (c : Lctx) (fn : String)
    (args : List Expr) : E (TExpr × List VTy) := do
  let some sig := lookup? g.fns fn | throw s!"unknown function {fn}"
  if args.length != sig.params.length then
    throw s!"call to {fn}: wrong argument count"
  let targs ← (args.zip sig.params).mapM (fun (a, pty) => do
    pure (← coerce (← elabExpr g c (some pty) a) (some pty)).1)
  pure (.callFn sig.idx targs, sig.results)

private partial def elabBuiltin (g : Genv) (c : Lctx) (fn : BFn)
    (args : List Expr) (spread : Bool) (mty : Option Ty) : E ER := do
  match fn, args with
  | .len, [x] => do
    match ← elabExpr g c none x with
    | .typed tx (.slice _) => pure (.typed (.lenSlice tx) (.int .i64))
    | .typed tx .str => pure (.typed (.lenStr tx) (.int .i64))
    | .cs s => pure (.ci s.size)
    | .typed tx (.arr n _) => pure (.typed (.lenArr tx n) (.int .i64))
    | _ => throw "len of a bad operand"
  | .cap, [x] => do
    let (tx, ty) ← coerce (← elabExpr g c none x) none
    match ty with
    | .slice _ => pure (.typed (.capSlice tx) (.int .i64))
    | _ => throw "cap of a non-slice"
  | .append, s :: rest => do
    if rest.isEmpty then throw "append needs elements"
    let (ts, sty) ← coerce (← elabExpr g c none s) none
    let .slice ety := sty | throw "append to a non-slice"
    if spread then do
      match rest with
      | [src] => do
        match ← elabExpr g c none src with
        | .typed tsrc (.slice e) =>
          if e == ety then
            pure (.typed (.appendSpread ts tsrc false ety) sty)
          else throw "append spread element type mismatch"
        | .typed tsrc .str =>
          if ety == .int .u8 then
            pure (.typed (.appendSpread ts tsrc true ety) sty)
          else throw "append string spread needs a byte slice"
        | .cs lit =>
          if ety == .int .u8 then
            pure (.typed (.appendSpread ts (.litStr lit) true ety) sty)
          else throw "append string spread needs a byte slice"
        | _ => throw "bad append spread source"
      | _ => throw "append spread takes one source"
    else do
      let tes ← rest.mapM (fun x => do
        pure (← coerce (← elabExpr g c (some ety) x) (some ety)).1)
      pure (.typed (.appendE ts tes ety) sty)
  | .make, len :: rest => do
    let some ty := mty | throw "make without a type"
    let .slice ety ← tyToVTy g ty | throw "make of a non-slice type"
    let tlen ← coerceAnyInt (← elabExpr g c none len)
    let tcap ← match rest with
      | [] => pure none
      | [ce] => pure (some (← coerceAnyInt (← elabExpr g c none ce)))
      | _ => throw "make takes two or three arguments"
    pure (.typed (.makeE ety tlen tcap) (.slice ety))
  | .copy, [dst, src] => do
    let (td, dty) ← coerce (← elabExpr g c none dst) none
    let .slice ety := dty | throw "copy into a non-slice"
    match ← elabExpr g c none src with
    | .typed tsrc (.slice e) =>
      if e == ety then pure (.typed (.copyE td tsrc false) (.int .i64))
      else throw "copy element type mismatch"
    | .typed tsrc .str =>
      if ety == .int .u8 then pure (.typed (.copyE td tsrc true) (.int .i64))
      else throw "copy from string needs a byte slice"
    | .cs lit =>
      if ety == .int .u8 then
        pure (.typed (.copyE td (.litStr lit) true) (.int .i64))
      else throw "copy from string needs a byte slice"
    | _ => throw "bad copy source"
  | .min, [x, y] => elabMinMax g c false x y
  | .max, [x, y] => elabMinMax g c true x y
  | _, _ => throw "bad builtin call"

private partial def elabMinMax (g : Genv) (c : Lctx) (isMax : Bool)
    (x y : Expr) : E ER := do
  let rx ← elabExpr g c none x
  let ry ← elabExpr g c none y
  match rx, ry with
  | .ci a, .ci b => pure (.ci (if isMax then max a b else min a b))
  | _, _ => do
    let (tx, tyx) ← match rx with
      | .typed _ _ => coerce rx none
      | _ => coerce rx (some (← coerce ry none).2)
    let (ty', _) ← coerce ry (some tyx)
    match tyx with
    | .int _ =>
      pure (.typed (if isMax then .maxE tx ty' else .minE tx ty') tyx)
    | _ => throw "min and max take integers"

/-- Elaborate an expression that an assignment can write to. -/
private partial def elabPlace (g : Genv) (c : Lctx) (e : Expr) :
    E (TPlace × VTy) := do
  match e with
  | .ident n =>
    match c.find? n with
    | some (slot, ty) => pure (.localP slot, ty)
    | none => throw s!"not an assignable place: {n}"
  | .field x f => do
    let (base, ty) ← elabPlace g c x
    let (si, viaPtr) ← match ty with
      | .strukt i => pure (← structAt g i, false)
      | .ptr i => pure (← structAt g i, true)
      | _ => throw "field access on a non-struct place"
    let some i := si.fieldNames.idxOf? f
      | throw s!"unknown field {f}"
    pure (.fieldP base i viaPtr, si.fieldTys[i]!)
  | .index x i => do
    let ti ← coerceAnyInt (← elabExpr g c none i)
    match elabPlace g c x with
    | .ok (base, .arr n e) => pure (.indexArrP base n ti, e)
    | .ok (_, .slice _) | .error _ =>
      match ← elabExpr g c none x with
      | .typed tx (.slice el) => pure (.indexSliceP tx ti, el)
      | _ => throw "index assignment into a non-slice"
    | .ok (_, _) => throw "index assignment into a bad place"
  | _ => throw "not an assignable place"

end

/-- Infer the declared type of a definition from its initializer. -/
private def defTy (r : ER) : E VTy :=
  match r with
  | .typed _ t => pure t
  | .ci _ => pure (.int .i64)
  | .cb _ => pure .bool
  | .cs _ => pure .str
  | .cnil => throw "cannot infer a type from nil"

mutual

private partial def elabStmts (g : Genv) (c : Lctx) (ss : List Stmt) :
    E (List TStmt × Lctx) := do
  let mut ctx := c
  let mut out : Array TStmt := #[]
  for s in ss do
    let (ts, ctx') ← elabStmt g ctx s
    out := out.push ts
    ctx := ctx'
  pure (out.toList, ctx)

/--
Elaborate a block: fresh scope inside.
The returned slot count carries the block's local slots up to the function total.
-/
private partial def elabBlock (g : Genv) (c : Lctx) (ss : List Stmt)
    (extraLoop : Bool := false) : E (List TStmt × Nat) := do
  let extra := if extraLoop then 1 else 0
  let inner := { c with scopes := [] :: c.scopes,
                        loopDepth := c.loopDepth + extra }
  let (ts, c') ← elabStmts g inner ss
  pure (ts, c'.nslots)

private partial def elabStmt (g : Genv) (c : Lctx) (s : Stmt) :
    E (TStmt × Lctx) := do
  match s with
  | .varDecl name ty val => do
    match ty with
    | some t => do
      let vt ← tyToVTy g t
      let init ← match val with
        | some e => do pure (← coerce (← elabExpr g c (some vt) e) (some vt)).1
        | none => pure (.zeroOf vt)
      if name == "_" then throw "blank var declaration"
      let (c', slot) := c.bind name vt
      pure (.newVar slot init, c')
    | none => do
      let some e := val | throw "var declaration without type or value"
      let r ← elabExpr g c none e
      let vt ← defTy r
      let (te, _) ← coerce r (some vt)
      let (c', slot) := c.bind name vt
      pure (.newVar slot te, c')
  | .define [name] e => do
    let r ← elabExpr g c none e
    if name == "_" then do
      let (te, _) ← coerce r none
      pure (.exprStmt te, c)
    else do
      let vt ← defTy r
      let (te, _) ← coerce r (some vt)
      let (c', slot) := c.bind name vt
      pure (.newVar slot te, c')
  | .define [n1, n2] e => do
    let some (fn, args) := (match e with
        | .call fn args => some (fn, args) | _ => none)
      | throw "two-value define needs a call"
    let (te, results) ← elabCall g c fn args
    let [t1, t2] := results | throw "two-value define needs two results"
    let mut ctx := c
    let mut s1 : Option Nat := none
    let mut s2 : Option Nat := none
    if n1 != "_" then
      let (c', slot) := ctx.bind n1 t1
      ctx := c'
      s1 := some slot
    if n2 != "_" then
      let (c', slot) := ctx.bind n2 t2
      ctx := c'
      s2 := some slot
    pure (.defineCall2 s1 s2 te, ctx)
  | .define _ _ => throw "bad define arity"
  | .assign [l] e => do
    if l == .ident "_" then do
      let (te, _) ← coerce (← elabExpr g c none e) none
      pure (.assign1 none te, c)
    else do
      let (p, pty) ← elabPlace g c l
      let (te, _) ← coerce (← elabExpr g c (some pty) e) (some pty)
      pure (.assign1 (some p) te, c)
  | .assign [l1, l2] e => do
    let some (fn, args) := (match e with
        | .call fn args => some (fn, args) | _ => none)
      | throw "two-value assignment needs a call"
    let (te, results) ← elabCall g c fn args
    let [t1, t2] := results | throw "two-value assignment needs two results"
    let pl : Expr → VTy → E (Option TPlace) := fun l t => do
      if l == Expr.ident "_" then pure (none : Option TPlace)
      else do
        let (p, pty) ← elabPlace g c l
        if pty == t then pure (some p) else throw "assignment type mismatch"
    pure (.assignCall2 (← pl l1 t1) (← pl l2 t2) te, c)
  | .assign _ _ => throw "bad assignment arity"
  | .opAssign op l e => do
    let (p, pty) ← elabPlace g c l
    let .int w := pty | throw "compound assignment to a non-integer"
    match op with
    | .shl | .shr => do
      let count ← coerceAnyInt (← elabExpr g c none e)
      pure (.opAssignSh (op == .shl) w p count, c)
    | _ => do
      let some aop := binToArith op | throw "bad compound operator"
      let (te, _) ← coerce (← elabExpr g c (some pty) e) (some pty)
      pure (.opAssignA aop w p te, c)
  | .incdec inc l => do
    let (p, pty) ← elabPlace g c l
    let .int w := pty | throw "increment of a non-integer"
    pure (.incdec inc w p, c)
  | .ifS cond thn els => do
    let (tc, _) ← coerce (← elabExpr g c none cond) (some .bool)
    let (tthn, n1) ← elabBlock g c thn
    let (tels, n2) ← match els with
      | some ss => elabBlock g { c with nslots := n1 } ss
      | none => pure ([], n1)
    pure (.ifS tc tthn tels, { c with nslots := n2 })
  | .forS ini cond post body => do
    let headScope := { c with scopes := [] :: c.scopes }
    let (tini, c1) ← match ini with
      | some s => do
        let (ts, c') ← elabStmt g headScope s
        pure (some ts, c')
      | none => pure (none, headScope)
    let tcond ← cond.mapM (fun e => do
      pure (← coerce (← elabExpr g c1 none e) (some .bool)).1)
    let (tpost, c2) ← match post with
      | some s => do
        let (ts, c') ← elabStmt g c1 s
        pure (some ts, c')
      | none => pure (none, c1)
    let (tbody, n) ← elabBlock g c2 body (extraLoop := true)
    pure (.forS tini tcond tpost tbody, { c with nslots := n })
  | .rangeS idx val over body => do
    let r ← elabExpr g c none over
    let (tover, oty) ← coerce r none
    let headScope := { c with scopes := [] :: c.scopes }
    let bindOpt (ctx : Lctx) (n : Option String) (t : VTy) :
        Lctx × Option Nat :=
      match n with
      | none => (ctx, none)
      | some "_" => (ctx, none)
      | some nm =>
        let (c', slot) := ctx.bind nm t
        (c', some slot)
    match oty with
    | .slice e => do
      let (c1, iSlot) := bindOpt headScope idx (.int .i64)
      let (c2, vSlot) := bindOpt c1 val e
      let (tbody, n) ← elabBlock g c2 body (extraLoop := true)
      pure (.rangeSlice iSlot vSlot tover tbody, { c with nslots := n })
    | .arr _ e => do
      let (c1, iSlot) := bindOpt headScope idx (.int .i64)
      let (c2, vSlot) := bindOpt c1 val e
      let (tbody, n) ← elabBlock g c2 body (extraLoop := true)
      pure (.rangeArr iSlot vSlot tover tbody, { c with nslots := n })
    | .int w => do
      if val.isSome then throw "range over an integer has no value"
      let (c1, iSlot) := bindOpt headScope idx (.int w)
      let (tbody, n) ← elabBlock g c1 body (extraLoop := true)
      pure (.rangeInt iSlot tover tbody, { c with nslots := n })
    | _ => throw "range over a bad operand"
  | .switchS tag cases dflt => do
    let (ttag, tagty) ← coerce (← elabExpr g c none tag) none
    match tagty with
    | .int _ | .bool | .str => pure ()
    | _ => throw "switch tag must be scalar"
    let mut nmax := c.nslots
    let mut tcases : Array (List TExpr × List TStmt) := #[]
    for (vals, body) in cases do
      let tvals ← vals.mapM (fun v => do
        pure (← coerce (← elabExpr g c (some tagty) v) (some tagty)).1)
      let (tbody, n) ← elabBlock g { c with nslots := nmax } body
      nmax := n
      tcases := tcases.push (tvals, tbody)
    let (tdflt, nfin) ← match dflt with
      | some ss => elabBlock g { c with nslots := nmax } ss
      | none => pure ([], nmax)
    pure (.switchS ttag tcases.toList tdflt, { c with nslots := nfin })
  | .breakS =>
    if c.loopDepth == 0 then throw "break outside a loop"
    else pure (.breakS, c)
  | .continueS =>
    if c.loopDepth == 0 then throw "continue outside a loop"
    else pure (.continueS, c)
  | .ret values => do
    if values.length != c.results.length then
      match values with
      | [.call fn args] => do
        let (te, results) ← elabCall g c fn args
        if results == c.results then pure (.retCall te, c)
        else throw "forwarded call results do not match"
      | _ => throw "wrong number of return values"
    else
      let tvs ← (values.zip c.results).mapM (fun (v, t) => do
        pure (← coerce (← elabExpr g c (some t) v) (some t)).1)
      pure (.ret tvs, c)
  | .exprStmt e => do
    match e with
    | .call fn args => do
      let (te, _) ← elabCall g c fn args
      pure (.exprStmt te, c)
    | _ => do
      let (te, _) ← coerce (← elabExpr g c none e) none
      pure (.exprStmt te, c)
  | .block body => do
    let (ts, n) ← elabBlock g c body
    pure (.blockS ts, { c with nslots := n })

end

/-- Resolve every constant declaration, iterating so declaration order does not matter, as in Go. -/
private def resolveConsts (decls : List ConstDecl) :
    E (List (String × CE)) := do
  let mut resolved : List (String × CE) := []
  let mut pending := decls
  for _ in [0:decls.length + 1] do
    let mut still : List ConstDecl := []
    for d in pending do
      match constEval resolved d.value with
      | .ok cv => do
        let cv ← match d.ty with
          | none => pure cv
          | some (.named t) =>
            match scalarIW? t, cv with
            | some w, .i v _ =>
              if w.fits v then pure (CE.i v (some w))
              else throw s!"constant {d.name} overflows {t}"
            | none, _ =>
              if t == "bool" then pure cv
              else if t == "string" then pure cv
              else throw s!"bad constant type for {d.name}"
            | _, _ => throw s!"bad constant value for {d.name}"
          | some _ => throw s!"bad constant type for {d.name}"
        resolved := resolved ++ [(d.name, cv)]
      | .error _ => still := still ++ [d]
    pending := still
  match pending with
  | [] => pure resolved
  | d :: _ =>
    match constEval resolved d.value with
    | .ok _ => throw s!"constant {d.name} did not resolve"
    | .error e => throw s!"constant {d.name}: {e}"

/-- Elaborate one whole program into the typed core. -/
def elabProgram (p : Program) : E TProgram := do
  let consts ← resolveConsts p.consts
  let mut g : Genv := { structs := [], consts, globals := [], fns := [] }
  for td in p.types, i in [0:p.types.length] do
    let si : StructInfo := { idx := i, fieldNames := td.fields.map (·.name),
                             fieldTys := #[] }
    g := { g with structs := g.structs ++ [(td.name, si)] }
  let mut structTable : Array (Array VTy) := #[]
  let mut filled : List (String × StructInfo) := []
  for td in p.types, i in [0:p.types.length] do
    let ftys ← td.fields.mapM (fun f => tyToVTy g f.ty)
    let si : StructInfo := { idx := i, fieldNames := td.fields.map (·.name),
                             fieldTys := ftys.toArray }
    filled := filled ++ [(td.name, si)]
    structTable := structTable.push ftys.toArray
  g := { g with structs := filled }
  let emptyCtx : Lctx :=
    { scopes := [], nslots := 0, loopDepth := 0, results := [] }
  let mut globalInits : Array TExpr := #[]
  for vd in p.vars do
    let vt ← match vd.ty with
      | some t => tyToVTy g t
      | none => do defTy (← elabExpr g emptyCtx none vd.value)
    let (te, _) ← coerce (← elabExpr g emptyCtx (some vt) vd.value) (some vt)
    g := { g with
           globals := g.globals ++ [(vd.name, globalInits.size, vt)] }
    globalInits := globalInits.push te
  let mut fnSigs : List (String × FnSig) := []
  for fd in p.funcs, i in [0:p.funcs.length] do
    let params ← fd.params.mapM (fun f => tyToVTy g f.ty)
    let results ← fd.results.mapM (tyToVTy g)
    if results.length > 2 then throw s!"{fd.name}: too many results"
    fnSigs := fnSigs ++ [(fd.name, { idx := i, params, results })]
  g := { g with fns := fnSigs }
  let mut funcs : Array TFunc := #[]
  for fd in p.funcs do
    let some sig := lookup? g.fns fd.name | throw "missing signature"
    let mut scope : List (String × Nat × VTy) := []
    for f in fd.params, pty in sig.params, i in [0:fd.params.length] do
      if f.name != "_" then
        scope := scope ++ [(f.name, i, pty)]
    let ctx : Lctx := { scopes := [scope], nslots := fd.params.length,
                        loopDepth := 0, results := sig.results }
    let (body, ctx') ← match elabStmts g ctx fd.body with
      | .ok r => pure r
      | .error e => throw s!"{fd.name}: {e}"
    funcs := funcs.push { name := fd.name, paramTys := sig.params,
                          results := sig.results, body,
                          nslots := ctx'.nslots }
  pure { structNames := (p.types.map (·.name)).toArray,
         structFields :=
           (p.types.map (fun t => (t.fields.map (·.name)).toArray)).toArray,
         structs := structTable, globalInits, funcs }

end Vego
