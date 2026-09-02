/-
Abstract syntax of Vego, the mechanically translatable Go subset.

The shapes here mirror section 8 of vego/SPECIFICATION.md one to one.
The JSON file is the portable artifact, and this AST is its Lean image.
Integer and character literals arrive as decimal strings in the JSON and are parsed into `Int` during decoding.
-/

deriving instance Repr for ByteArray

namespace Vego

/--
Scalar integer widths of the subset.
`int` is 64-bit signed.
-/
inductive IW where
  | u8 | u16 | u32 | u64 | i32 | i64
  deriving Repr, BEq, DecidableEq

/-- Binary operators, including comparisons and short-circuit ops. -/
inductive BinOp where
  | add | sub | mul | quo | rem
  | shl | shr | band | bor | bxor | andNot
  | land | lor
  | eq | ne | lt | le | gt | ge
  deriving Repr, BEq, DecidableEq

/-- Unary operators: arithmetic negation, bit complement, logical not, and address-of (legal only on direct call arguments). -/
inductive UnOp where
  | neg | bnot | lnot | addr
  deriving Repr, BEq, DecidableEq

/-- The builtin functions of section 5.1. -/
inductive BFn where
  | len | cap | append | make | copy | min | max
  deriving Repr, BEq, DecidableEq

mutual

/--
Type references.
Array lengths are constant expressions.
-/
inductive Ty where
  | named (name : String)
  | slice (elem : Ty)
  | arr (len : Expr) (elem : Ty)
  | structRef (name : String)
  | ptr (name : String)
  deriving Repr, BEq

/--
Expressions.
String literal bytes are the UTF-8 encoding of the JSON string value.
The checker keeps the program ASCII-clean where byte identity matters.
-/
inductive Expr where
  | intLit (v : Int)
  | strLit (bytes : ByteArray)
  | boolLit (b : Bool)
  | ident (name : String)
  | field (x : Expr) (name : String)
  | index (x : Expr) (idx : Expr)
  | sliceE (x : Expr) (lo : Option Expr) (hi : Option Expr)
  | call (fn : String) (args : List Expr)
  | builtin (fn : BFn) (args : List Expr) (spread : Bool) (ty : Option Ty)
  | conv (ty : Ty) (x : Expr)
  | unary (op : UnOp) (x : Expr)
  | binary (op : BinOp) (x : Expr) (y : Expr)
  | compositeS (ty : Ty) (fields : List (String × Expr))
  | compositeL (ty : Ty) (elems : List Expr)
  deriving Repr, BEq

/-- Statements, in the exact shapes the JSON grammar allows. -/
inductive Stmt where
  | varDecl (name : String) (ty : Option Ty) (value : Option Expr)
  | define (names : List String) (value : Expr)
  | assign (lhs : List Expr) (value : Expr)
  | opAssign (op : BinOp) (lhs : Expr) (value : Expr)
  | incdec (inc : Bool) (lhs : Expr)
  | ifS (cond : Expr) (thn : List Stmt) (els : Option (List Stmt))
  | forS (init : Option Stmt) (cond : Option Expr) (post : Option Stmt)
         (body : List Stmt)
  | rangeS (idx : Option String) (val : Option String) (over : Expr)
           (body : List Stmt)
  | switchS (tag : Expr) (cases : List (List Expr × List Stmt))
            (dflt : Option (List Stmt))
  | breakS
  | continueS
  | ret (values : List Expr)
  | exprStmt (value : Expr)
  | block (body : List Stmt)
  deriving Repr, BEq

end

/-- A struct field or a function parameter. -/
structure Field where
  name : String
  ty : Ty
  deriving Repr, BEq

structure ConstDecl where
  name : String
  ty : Option Ty
  value : Expr
  deriving Repr, BEq

structure VarDecl where
  name : String
  ty : Option Ty
  value : Expr
  deriving Repr, BEq

structure TypeDecl where
  name : String
  fields : List Field
  deriving Repr, BEq

structure FuncDecl where
  name : String
  params : List Field
  results : List Ty
  body : List Stmt
  deriving Repr, BEq

structure Program where
  package : String
  consts : List ConstDecl
  vars : List VarDecl
  types : List TypeDecl
  funcs : List FuncDecl
  deriving Repr, BEq

end Vego
