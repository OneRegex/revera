import Vego.Elab
import Vego.Machine

namespace Vego

private def lenProgram : Program :=
  let intTy := Ty.named "int"
  let arrayTy := Ty.arr (.intLit 2) intTy
  let field := Expr.field (.ident "s") "N"
  { package := "len_test", consts := [], vars := [],
    types := [{ name := "State", fields := [{ name := "N", ty := intTy }] }],
    funcs := [
      { name := "bump", params := [{ name := "s", ty := .ptr "State" }], results := [arrayTy],
        body := [.incdec true field, .ret [.compositeL arrayTy [.intLit 1, .intLit 2]]] },
      { name := "Effect", params := [{ name := "s", ty := .ptr "State" }], results := [intTy],
        body := [.define ["n"] (.builtin .len [.call "bump" [.ident "s"]] false none),
          .ret [.binary .add (.binary .mul field (.intLit 10)) (.ident "n")]] },
      { name := "Constant", params := [{ name := "a", ty := .slice arrayTy }], results := [intTy],
        body := [.ret [.builtin .len [.index (.ident "a") (.intLit 0)] false none]] }
    ] }

private def lenReport : Except String (Int × Int) := do
  let tp ← elabProgram lenProgram
  let m ← (Machine.init tp).mapError (fun e => s!"{repr e}")
  let (m, cell) := m.alloc (m.zeroStruct "State")
  let (effect, _) ← (m.call "Effect" [.ptr cell 0 []]).mapError (fun e => s!"{repr e}")
  let (constant, _) ← (m.call "Constant" [.slice none 0 0 0]).mapError (fun e => s!"{repr e}")
  match effect, constant with
  | [.i a], [.i b] => pure (a, b)
  | _, _ => throw "unexpected result types"

theorem array_len_evaluation :
    (match lenReport with | .ok (a, b) => a == 12 && b == 2 | .error _ => false) = true := by native_decide

end Vego
