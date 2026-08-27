/-
Decoder from the vego2json output into the Lean AST.

The decoder is total: it recurses on an explicit depth budget, so every theorem downstream rests only on ordinary definitions.
The budget is far above the real nesting depth of the two shipped programs.
A run out of budget is reported as an error, never as silence.
-/

import Lean.Data.Json
import Vego.Ast

namespace Vego

open Lean (Json)

abbrev D := Except String

private def jStr (j : Json) (key : String) : D String := do
  match (← j.getObjVal? key).getStr? with
  | .ok s => pure s
  | .error e => throw s!"field {key}: {e}"

private def jArr (j : Json) (key : String) : D (Array Json) := do
  match (← j.getObjVal? key).getArr? with
  | .ok a => pure a
  | .error e => throw s!"field {key}: {e}"

private def jOpt (j : Json) (key : String) : D (Option Json) :=
  match j.getObjVal? key with
  | .ok .null => pure none
  | .ok v => pure (some v)
  | .error _ => pure none

private def jBoolD (j : Json) (key : String) : D Bool :=
  match j.getObjVal? key with
  | .ok (.bool b) => pure b
  | .ok .null => pure false
  | .ok _ => throw s!"field {key}: expected bool"
  | .error _ => pure false

private def kindOf (j : Json) : D String := jStr j "k"

private def decInt (s : String) : D Int :=
  match s.toInt? with
  | some v => pure v
  | none => throw s!"bad integer literal {s}"

private def decBinOp (s : String) : D BinOp :=
  match s with
  | "+" => pure .add | "-" => pure .sub | "*" => pure .mul
  | "/" => pure .quo | "%" => pure .rem
  | "<<" => pure .shl | ">>" => pure .shr
  | "&" => pure .band | "|" => pure .bor | "^" => pure .bxor
  | "&^" => pure .andNot
  | "&&" => pure .land | "||" => pure .lor
  | "==" => pure .eq | "!=" => pure .ne
  | "<" => pure .lt | "<=" => pure .le
  | ">" => pure .gt | ">=" => pure .ge
  | _ => throw s!"unknown binary operator {s}"

private def decAssignOp (s : String) : D BinOp :=
  match s with
  | "+=" => pure .add | "-=" => pure .sub | "*=" => pure .mul
  | "/=" => pure .quo | "%=" => pure .rem
  | "<<=" => pure .shl | ">>=" => pure .shr
  | "&=" => pure .band | "|=" => pure .bor | "^=" => pure .bxor
  | "&^=" => pure .andNot
  | _ => throw s!"unknown assignment operator {s}"

private def decUnOp (s : String) : D UnOp :=
  match s with
  | "-" => pure .neg | "^" => pure .bnot
  | "!" => pure .lnot | "&" => pure .addr
  | _ => throw s!"unknown unary operator {s}"

private def decBFn (s : String) : D BFn :=
  match s with
  | "len" => pure .len | "cap" => pure .cap | "append" => pure .append
  | "make" => pure .make | "copy" => pure .copy
  | "min" => pure .min | "max" => pure .max
  | _ => throw s!"unknown builtin {s}"

mutual

private def decTy (fuel : Nat) (j : Json) : D Ty := do
  match fuel with
  | 0 => throw "type nesting too deep"
  | fuel + 1 =>
    match ← kindOf j with
    | "named" => pure (.named (← jStr j "name"))
    | "slice" => pure (.slice (← decTy fuel (← j.getObjVal? "elem")))
    | "array" =>
      pure (.arr (← decExpr fuel (← j.getObjVal? "len"))
                 (← decTy fuel (← j.getObjVal? "elem")))
    | "struct_ref" => pure (.structRef (← jStr j "name"))
    | "ptr" => pure (.ptr (← jStr j "name"))
    | k => throw s!"unknown type kind {k}"

private def decExpr (fuel : Nat) (j : Json) : D Expr := do
  match fuel with
  | 0 => throw "expression nesting too deep"
  | fuel + 1 =>
    match ← kindOf j with
    | "int" => pure (.intLit (← decInt (← jStr j "value")))
    | "char" => pure (.intLit (← decInt (← jStr j "value")))
    | "str" => pure (.strLit (← jStr j "value").toUTF8)
    | "bool" =>
      match ← j.getObjVal? "value" with
      | .bool b => pure (.boolLit b)
      | _ => throw "bool literal without bool value"
    | "ident" => pure (.ident (← jStr j "name"))
    | "field" =>
      pure (.field (← decExpr fuel (← j.getObjVal? "x")) (← jStr j "name"))
    | "index" =>
      pure (.index (← decExpr fuel (← j.getObjVal? "x"))
                   (← decExpr fuel (← j.getObjVal? "index")))
    | "slice_expr" =>
      pure (.sliceE (← decExpr fuel (← j.getObjVal? "x"))
                    (← decOptExpr fuel (← jOpt j "lo"))
                    (← decOptExpr fuel (← jOpt j "hi")))
    | "call" =>
      pure (.call (← jStr j "fn") (← decExprs fuel (← jArr j "args")))
    | "builtin" =>
      pure (.builtin (← decBFn (← jStr j "fn"))
                     (← decExprs fuel (← jArr j "args"))
                     (← jBoolD j "spread")
                     (← decOptTy fuel (← jOpt j "type")))
    | "conv" =>
      pure (.conv (← decTy fuel (← j.getObjVal? "type"))
                  (← decExpr fuel (← j.getObjVal? "x")))
    | "unary" =>
      pure (.unary (← decUnOp (← jStr j "op"))
                   (← decExpr fuel (← j.getObjVal? "x")))
    | "binary" =>
      pure (.binary (← decBinOp (← jStr j "op"))
                    (← decExpr fuel (← j.getObjVal? "x"))
                    (← decExpr fuel (← j.getObjVal? "y")))
    | "composite" => do
      let ty ← decTy fuel (← j.getObjVal? "type")
      match ← jOpt j "fields" with
      | some fj =>
        match fj.getArr? with
        | .ok fields => pure (.compositeS ty (← decNamed fuel fields))
        | .error e => throw s!"composite fields: {e}"
      | none =>
        match ← jOpt j "elems" with
        | some ej =>
          match ej.getArr? with
          | .ok elems => pure (.compositeL ty (← decExprs fuel elems))
          | .error e => throw s!"composite elems: {e}"
        | none => pure (.compositeL ty [])
    | k => throw s!"unknown expression kind {k}"

private def decOptExpr (fuel : Nat) (j : Option Json) : D (Option Expr) := do
  match j with
  | none => pure none
  | some j => pure (some (← decExpr fuel j))

private def decOptTy (fuel : Nat) (j : Option Json) : D (Option Ty) := do
  match j with
  | none => pure none
  | some j => pure (some (← decTy fuel j))

private def decExprs (fuel : Nat) (js : Array Json) : D (List Expr) := do
  match fuel with
  | 0 => throw "expression nesting too deep"
  | fuel + 1 =>
    let mut out := #[]
    for j in js do
      out := out.push (← decExpr fuel j)
    pure out.toList

private def decNamed (fuel : Nat) (js : Array Json) :
    D (List (String × Expr)) := do
  match fuel with
  | 0 => throw "expression nesting too deep"
  | fuel + 1 =>
    let mut out := #[]
    for j in js do
      out := out.push ((← jStr j "name", ← decExpr fuel (← j.getObjVal? "value")))
    pure out.toList

end

mutual

private def decStmt (fuel : Nat) (j : Json) : D Stmt := do
  match fuel with
  | 0 => throw "statement nesting too deep"
  | fuel + 1 =>
    match ← kindOf j with
    | "var_decl" =>
      pure (.varDecl (← jStr j "name")
                     (← decOptTy fuel (← jOpt j "type"))
                     (← decOptExpr fuel (← jOpt j "value")))
    | "define" => do
      let names ← (← jArr j "names").mapM fun n =>
        match n.getStr? with
        | .ok s => pure s
        | .error e => throw s!"define name: {e}"
      pure (.define names.toList (← decExpr fuel (← j.getObjVal? "value")))
    | "assign" =>
      pure (.assign (← decExprs fuel (← jArr j "lhs"))
                    (← decExpr fuel (← j.getObjVal? "value")))
    | "op_assign" =>
      pure (.opAssign (← decAssignOp (← jStr j "op"))
                      (← decExpr fuel (← j.getObjVal? "lhs"))
                      (← decExpr fuel (← j.getObjVal? "value")))
    | "incdec" =>
      pure (.incdec ((← jStr j "op") == "++")
                    (← decExpr fuel (← j.getObjVal? "lhs")))
    | "if" =>
      pure (.ifS (← decExpr fuel (← j.getObjVal? "cond"))
                 (← decStmts fuel (← jArr j "then"))
                 (← decOptStmts fuel (← jOpt j "else")))
    | "for" =>
      pure (.forS (← decOptStmt fuel (← jOpt j "init"))
                  (← decOptExpr fuel (← jOpt j "cond"))
                  (← decOptStmt fuel (← jOpt j "post"))
                  (← decStmts fuel (← jArr j "body")))
    | "range" => do
      let name : Option Json → D (Option String) := fun oj =>
        match oj with
        | none => pure none
        | some v =>
          match v.getStr? with
          | .ok s => pure (some s)
          | .error e => throw s!"range variable: {e}"
      pure (.rangeS (← name (← jOpt j "idx")) (← name (← jOpt j "val"))
                    (← decExpr fuel (← j.getObjVal? "over"))
                    (← decStmts fuel (← jArr j "body")))
    | "switch" => do
      let cases ← (← jArr j "cases").mapM fun c => do
        pure ((← decExprs fuel (← jArr c "values")),
              (← decStmts fuel (← jArr c "body")))
      pure (.switchS (← decExpr fuel (← j.getObjVal? "tag"))
                     cases.toList
                     (← decOptStmts fuel (← jOpt j "default")))
    | "break" => pure .breakS
    | "continue" => pure .continueS
    | "return" => pure (.ret (← decExprs fuel (← jArr j "values")))
    | "expr_stmt" => pure (.exprStmt (← decExpr fuel (← j.getObjVal? "value")))
    | "block" => pure (.block (← decStmts fuel (← jArr j "body")))
    | k => throw s!"unknown statement kind {k}"

private def decOptStmt (fuel : Nat) (j : Option Json) : D (Option Stmt) := do
  match j with
  | none => pure none
  | some j => pure (some (← decStmt fuel j))

private def decStmts (fuel : Nat) (js : Array Json) : D (List Stmt) := do
  match fuel with
  | 0 => throw "statement nesting too deep"
  | fuel + 1 =>
    let mut out := #[]
    for j in js do
      out := out.push (← decStmt fuel j)
    pure out.toList

private def decOptStmts (fuel : Nat) (j : Option Json) :
    D (Option (List Stmt)) := do
  match j with
  | none => pure none
  | some j =>
    match j.getArr? with
    | .ok a => pure (some (← decStmts fuel a))
    | .error e => throw s!"statement list: {e}"

end

private def declFuel : Nat := 1000000

private def decField (j : Json) : D Field := do
  pure { name := ← jStr j "name", ty := ← decTy declFuel (← j.getObjVal? "type") }

/-- Decode one whole program object. -/
def decodeProgram (text : String) : D Program := do
  let j ← Json.parse text
  let version ← j.getObjVal? "vego"
  if version != Json.num 1 then
    throw "unsupported vego version"
  let consts ← (← jArr j "consts").mapM fun c => do
    pure { name := ← jStr c "name",
           ty := ← decOptTy declFuel (← jOpt c "type"),
           value := ← decExpr declFuel (← c.getObjVal? "value")
           : ConstDecl }
  let vars ← (← jArr j "vars").mapM fun v => do
    pure { name := ← jStr v "name",
           ty := ← decOptTy declFuel (← jOpt v "type"),
           value := ← decExpr declFuel (← v.getObjVal? "value")
           : VarDecl }
  let types ← (← jArr j "types").mapM fun t => do
    pure { name := ← jStr t "name",
           fields := (← (← jArr t "fields").mapM decField).toList
           : TypeDecl }
  let funcs ← (← jArr j "funcs").mapM fun f => do
    pure { name := ← jStr f "name",
           params := (← (← jArr f "params").mapM decField).toList,
           results := (← (← jArr f "results").mapM (decTy declFuel)).toList,
           body := ← decStmts declFuel (← jArr f "body")
           : FuncDecl }
  pure { package := ← jStr j "package",
         consts := consts.toList, vars := vars.toList,
         types := types.toList, funcs := funcs.toList }

end Vego
