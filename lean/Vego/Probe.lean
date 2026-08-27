/-
The probe harness: the Lean instantiation of probe_host.go.

It runs the same call matrix as the Go, Zig, C++ and Rust probe hosts, against the interpreted probe program.
It formats the same report lines.
The theorem in Theorems.lean states that the lines equal the output of the Go original byte for byte.
-/

import Vego.Machine
import Vego.Data

namespace Vego

abbrev P := Except String

private def tr {α : Type} (x : Except Trap α) : P α :=
  match x with
  | .ok v => pure v
  | .error t => throw s!"trap: {repr t}"

private def call1 (m : Machine) (name : String) (args : List Val) :
    P (Val × Machine) := do
  match ← tr (m.call name args) with
  | ([v], m') => pure (v, m')
  | _ => throw s!"{name}: expected one result"

private def call2 (m : Machine) (name : String) (args : List Val) :
    P (Val × Val × Machine) := do
  match ← tr (m.call name args) with
  | ([v1, v2], m') => pure (v1, v2, m')
  | _ => throw s!"{name}: expected two results"

private def asInt (v : Val) : P Int :=
  match v with
  | .i n => pure n
  | _ => throw "expected an integer result"

private def asBool (v : Val) : P Bool :=
  match v with
  | .b x => pure x
  | _ => throw "expected a bool result"

private def str (s : String) : Val := .s s.toUTF8

/-- Run the probe call matrix and format the report lines. -/
def probeReport (tp : TProgram) : P (List String) := do
  let mut m ← tr (Machine.init tp)
  let mut out : Array String := #[]
  let minI64 : Int := -9223372036854775808
  for (a, b) in [(minI64, -1), (7, -2), (-7, 2), (minI64, 1), (1, minI64)] do
    let (q, r, m') ← call2 m "DivMod" [.i a, .i b]
    m := m'
    out := out.push s!"divmod {a} {b} = {← asInt q} {← asInt r}"
  let (q32, r32, m') ← call2 m "DivMod32" [.i (-2147483648), .i (-1)]
  m := m'
  out := out.push s!"divmod32 = {← asInt q32} {← asInt r32}"
  let (q32b, r32b, m') ← call2 m "DivMod32" [.i 9, .i (-4)]
  m := m'
  out := out.push s!"divmod32b = {← asInt q32b} {← asInt r32b}"
  let (bh, m') ← call1 m "BytesProbe" [str "hello"]
  m := m'
  let (be, m') ← call1 m "BytesProbe" [str ""]
  m := m'
  out := out.push s!"bytes = {← asInt bh} {← asInt be}"
  let (m', c1) := m.alloc (m.zeroStruct "Counter")
  m := m'
  let (rg, m') ← call1 m "RangeProbe" [.ptr c1 0 []]
  m := m'
  out := out.push s!"range = {← asInt rg}"
  let (m', xs) := m.mkSlice #[.i 3, .i 5, .i 7]
  m := m'
  let (rv, m') ← call1 m "RangeValProbe" [xs]
  m := m'
  out := out.push s!"rangeval = {← asInt rv}"
  let (ri, m') ← call1 m "RangeIntProbe" [.i 5]
  m := m'
  out := out.push s!"rangeint = {← asInt ri}"
  let (pa, m') ← call1 m "PartialArray" []
  m := m'
  out := out.push s!"partial = {← asInt pa}"
  let mkTagged (a b : String) (n : Int) : Val :=
    .strukt #[.arr #[str a, str b], .i n]
  let eqTag (mm : Machine) (x y : Val) : P (Int × Machine) := do
    let (r, mm') ← call1 mm "TaggedEq" [x, y]
    pure (if ← asBool r then 1 else 0, mm')
  let (e1, m') ← eqTag m (mkTagged "a" "b" 1) (mkTagged "a" "b" 1)
  m := m'
  let (e2, m') ← eqTag m (mkTagged "a" "b" 1) (mkTagged "a" "c" 1)
  m := m'
  let (e3, m') ← eqTag m (mkTagged "a" "b" 1) (mkTagged "a" "b" 2)
  m := m'
  out := out.push s!"tagged = {e1} {e2} {e3}"
  let counterCall (mm : Machine) (fn : String) : P (Int × Machine) := do
    let (mm1, cc) := mm.alloc (mm.zeroStruct "Counter")
    let (r, mm2) ← call1 mm1 fn [.ptr cc 0 []]
    pure (← asInt r, mm2)
  let (oa, m') ← counterCall m "OrderArgs"
  m := m'
  out := out.push s!"orderargs = {oa}"
  let (ob, m') ← counterCall m "OrderBinary"
  m := m'
  out := out.push s!"orderbinary = {ob}"
  let (oi, m') ← counterCall m "OrderIndex"
  m := m'
  out := out.push s!"orderindex = {oi}"
  let (sp, m') ← call1 m "SpareProbe" []
  m := m'
  out := out.push s!"spare = {← asInt sp}"
  let (nl, m') ← call1 m "NilProbe" []
  m := m'
  out := out.push s!"nil = {← asInt nl}"
  let (w1, m') ← call1 m "WrapProbe" [.i minI64, .i 3]
  m := m'
  let (w2, m') ← call1 m "WrapProbe" [.i 7, .i (-9)]
  m := m'
  out := out.push s!"wrap = {← asInt w1} {← asInt w2}"
  let (n1, m') ← call1 m "Narrow32" [.i (-2147483648), .i (-1)]
  m := m'
  let (n2, m') ← call1 m "Narrow32" [.i (-17), .i 5]
  m := m'
  out := out.push s!"narrow32 = {← asInt n1} {← asInt n2}"
  let (wu, m') ← call1 m "WrapU8" [.i 3, .i 200]
  m := m'
  out := out.push s!"wrapu8 = {← asInt wu}"
  let (an, m') ← call1 m "AndNotProbe" [.i 0xF0F0F0F0, .i 0xFF00FF00]
  m := m'
  out := out.push s!"andnot = {← asInt an}"
  let (sh, m') ← call1 m "ShiftProbe" [.i 0x8000000000000001, .i 7]
  m := m'
  out := out.push s!"shift = {← asInt sh}"
  let (cv1, m') ← call1 m "ConvProbe" [.i (-99)]
  m := m'
  let (cv2, m') ← call1 m "ConvProbe" [.i 300]
  m := m'
  out := out.push s!"conv = {← asInt cv1} {← asInt cv2}"
  let (sw, m') ← call1 m "SubWrite" [.i 4]
  m := m'
  out := out.push s!"subwrite = {← asInt sw}"
  let (ao, m') ← counterCall m "AndNotOrder"
  m := m'
  out := out.push s!"andnotorder = {ao}"
  let (za, m') ← call1 m "ZeroArray" []
  m := m'
  out := out.push s!"zeroarray = {← asInt za}"
  let (mk64, m') ← call1 m "MakeU64" [.i 6]
  m := m'
  out := out.push s!"makeu64 = {← asInt mk64}"
  let (pk, m') ← counterCall m "PickArray"
  m := m'
  out := out.push s!"pickarray = {pk}"
  pure out.toList

/-- The reference output of the Go original, one line per probe. -/
def probeExpected : List String :=
  ((include_str "../data/probe.expected").trim.splitOn "\n")

/-- The checked probe program. -/
def probeChecked : Except String TProgram :=
  match decodeProgram probeJsonText with
  | .ok p => elabProgram p
  | .error e => .error e

/-- The whole probe pipeline, as one boolean. -/
def probeAgrees : Bool :=
  match probeChecked with
  | .ok tp =>
    match probeReport tp with
    | .ok lines => lines == probeExpected
    | .error _ => false
  | .error _ => false

end Vego
