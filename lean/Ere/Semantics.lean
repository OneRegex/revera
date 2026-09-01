/-
The match semantics of the specification: existence of a match (section 4.2), the empty-occurrence rule
(section 8.5), anchors and newline mode (sections 9.2, 12.3, 12.4), the case-insensitive closure (section
10.2), match selection (section 4.3) and capture reporting (sections 12.5 to 12.7).

The definition is deliberately naive.
`parses` lists every derivation of a pattern node over exactly one span of the subject.
`bestAt` orders those derivations by the rules of section 4.3 and takes the first.
`assign` reads the captures off the chosen derivation by the rules of section 12.7.
Nothing here is an algorithm an engine would use; it is the contract an engine must meet.

The enumeration is memoized on (node, start, end) and counts its work, so that a replay over a corpus stays
tractable and terminates under a budget. Both are bookkeeping; they do not change which derivations exist.
-/

import Ere.Syntax
import Std.Data.HashMap

namespace Ere

/-- Execution flags, section 11.3. -/
structure EFlags where
  notbol : Bool := false
  noteol : Bool := false
  deriving Repr, BEq, Inhabited

/-- The flag bits of the engine's interface: 1 NotBOL, 2 NotEOL. -/
def EFlags.ofBits (n : Nat) : EFlags := { notbol := n % 2 == 1, noteol := (n / 2) % 2 == 1 }

/-- A subject decoded into characters, with the byte offset of every character boundary (section 12.6). -/
structure Subject where
  chars : Array Chr
  byteAt : Array Nat
  deriving Repr, Inhabited

def Subject.ofBytes (bs : ByteArray) : Option Subject :=
  (decodeUtf8 bs).map fun (chars, byteAt) => { chars, byteAt }

/-- Everything a match relation depends on besides the pattern. -/
structure Ctx where
  loc : Locale
  flags : CFlags
  eflags : EFlags
  subj : Subject

/-! ## Atoms -/

/--
Section 10.2. With `REG_ICASE`, a character `t` is accepted when some character `c'` accepted
case-sensitively (`S c'`) has `t` among `c'` and its two counterparts, so `t` is `c'` or a counterpart of
`c'` lands on `t`. Without the flag, acceptance is `S` itself.
-/
def icaseClosure (loc : Locale) (icase : Bool) (S : Chr → Bool) (t : Chr) : Bool :=
  S t || (icase && (loc.casePreimages t).any S)

/-- The declarative form of the closure rule, for the statement `posix_icaseClosure_iff` in `Lemmas.lean`. -/
def ClosureProp (loc : Locale) (S : Chr → Bool) (t : Chr) : Prop :=
  ∃ c', S c' = true ∧ (t = c' ∨ t = loc.toUpper c' ∨ t = loc.toLower c')

/-- An ordinary or quoted character, section 8.1. -/
def charAccepts (ctx : Ctx) (c t : Chr) : Bool :=
  icaseClosure ctx.loc ctx.flags.icase (· == c) t

/-- Dot, section 8.1: any character but NUL, and not newline under `REG_NEWLINE` (section 12.3). -/
def anyAccepts (ctx : Ctx) (t : Chr) : Bool :=
  t != 0 && !(ctx.flags.newline && t == 10)

/-- One list member against one character, sections 7.3 to 7.7. -/
def itemAcceptsOne (loc : Locale) (item : BracketItem) (t : Chr) : Bool :=
  match item with
  | .char c => t == c
  | .range lo hi => lo ≤ t && t ≤ hi
  | .elem seq => seq == [t]
  | .equiv seq => loc.primaryEqual [t] seq
  | .cls name => loc.classMember name t

/-- Case-sensitive membership of one character in the positive list. -/
def positiveOne (loc : Locale) (b : Bracket) (t : Chr) : Bool :=
  b.items.any (itemAcceptsOne loc · t)

/--
A bracket expression against one character, section 7.3.
A non-matching list is the inverse of its positive list, the case closure is taken after that inverse
(section 10.2), and under `REG_NEWLINE` no non-matching list matches newline (section 12.3).
-/
def bracketAcceptsOne (ctx : Ctx) (b : Bracket) (t : Chr) : Bool :=
  let caseSensitive := fun t =>
    if b.negated then !positiveOne ctx.loc b t else positiveOne ctx.loc b t
  icaseClosure ctx.loc ctx.flags.icase caseSensitive t &&
    !(b.negated && ctx.flags.newline && t == 10)

/-- `ts` is `seq` with each character replaced by itself or a counterpart, section 10.2. -/
def seqCounterpart (loc : Locale) (icase : Bool) (ts seq : List Chr) : Bool :=
  ts.length == seq.length &&
    (ts.zip seq).all fun (t, e) => icaseClosure loc icase (· == e) t

/-- Every sequence that `ts` could be the case image of: each position is itself or one of its preimages. -/
def casePreimageSeqs (loc : Locale) (icase : Bool) : List Chr → List (List Chr)
  | [] => [[]]
  | t :: rest =>
    let heads := if icase then t :: loc.casePreimages t else [t]
    let tails := casePreimageSeqs loc icase rest
    heads.flatMap fun h => tails.map (h :: ·)

/--
A bracket expression against a multi-character collating element, sections 7.4 and 7.5.

Section 14.3 leaves two choices to the implementation, and this is the pair this implementation makes:
only an explicit `[.element.]` or `[=element=]` member recognizes a multi-character element, so an
ordinary list character or a class never does; and a non-matching list never consumes more than one
character.
-/
def bracketAcceptsMulti (ctx : Ctx) (b : Bracket) (ts : List Chr) : Bool :=
  !b.negated && ts.length ≥ 2 && ts.length ≤ ctx.loc.maxElementLength &&
    b.items.any fun item =>
      match item with
      | .elem seq => seqCounterpart ctx.loc ctx.flags.icase ts seq
      | .equiv seq =>
        (casePreimageSeqs ctx.loc ctx.flags.icase ts).any fun ts' =>
          ctx.loc.isCollatingElement ts' && ctx.loc.primaryEqual ts' seq
      | _ => false

/-- `^` at character boundary `i`, sections 9.2, 12.3 and 12.4. -/
def atBOL (ctx : Ctx) (i : Nat) : Bool :=
  if i == 0 then !ctx.eflags.notbol
  else ctx.flags.newline && ctx.subj.chars[i - 1]! == 10

/-- `$` at character boundary `i`. -/
def atEOL (ctx : Ctx) (i : Nat) : Bool :=
  if i == ctx.subj.chars.size then !ctx.eflags.noteol
  else ctx.flags.newline && ctx.subj.chars[i]! == 10

/-! ## Derivations -/

/--
One derivation of a pattern node over the span `[i, j)` of the subject, in characters.
An `alt` records the branch it took, and a `rep` records the derivation of each occurrence in order,
plus the pre-order id of its node and its preference, which the selection order reads.
-/
inductive PTree where
  | leaf (i j : Nat)
  | group (i j : Nat) (t : PTree)
  | alt (i j : Nat) (branch : Nat) (t : PTree)
  | cat (i j : Nat) (a b : PTree)
  | rep (id : Nat) (minimal : Bool) (i j : Nat) (ts : List PTree)
  deriving Repr, Inhabited

def PTree.i : PTree → Nat
  | .leaf i _ | .group i _ _ | .alt i _ _ _ | .cat i _ _ _ | .rep _ _ i _ _ => i

def PTree.j : PTree → Nat
  | .leaf _ j | .group _ j _ | .alt _ j _ _ | .cat _ j _ _ | .rep _ _ _ j _ => j

def PTree.span (t : PTree) : Nat := t.j - t.i

/-- The number of nodes, which gives every node its pre-order id. -/
def Ere.size : Ere → Nat
  | .char _ | .any | .bracket _ | .bol | .eol => 1
  | .group _ e => 1 + e.size
  | .cat a b | .alt a b => 1 + a.size + b.size
  | .rep _ _ _ e => 1 + e.size

/-- The pre-order ids of the shortest-preferring repetitions, ascending. -/
def Ere.minimalIds (e : Ere) (id : Nat) : List Nat :=
  match e with
  | .char _ | .any | .bracket _ | .bol | .eol => []
  | .group _ e => e.minimalIds (id + 1)
  | .cat a b | .alt a b => a.minimalIds (id + 1) ++ b.minimalIds (id + 1 + a.size)
  | .rep _ _ minimal e => (if minimal then [id] else []) ++ e.minimalIds (id + 1)

/-- The numbers of the groups inside `e`, including `e` itself when it is a group. -/
def Ere.groupsIn : Ere → List Nat
  | .char _ | .any | .bracket _ | .bol | .eol => []
  | .group idx e => idx :: e.groupsIn
  | .cat a b | .alt a b => a.groupsIn ++ b.groupsIn
  | .rep _ _ _ e => e.groupsIn

/-- The memo table and the work counter of one enumeration. -/
structure Memo where
  table : Std.HashMap (Nat × Nat × Nat) (List PTree) := {}
  work : Nat := 0
  budget : Nat
  exhausted : Bool := false

abbrev SM := StateM Memo

/-- Count one unit of work. Past the budget the enumeration stops producing derivations and reports it. -/
def tick : SM Bool :=
  modifyGet fun m =>
    let m := { m with work := m.work + 1 }
    if m.work > m.budget then (false, { m with exhausted := true }) else (true, m)

/-- Every way to split `[i, j)` so that `a` covers the front and `b` the rest, section 8.3. -/
def catParses (pa pb : Nat → Nat → SM (List PTree)) (i j : Nat) : SM (List PTree) := do
  let mut out : Array PTree := #[]
  for m in [i:j+1] do
    let heads ← pa i m
    if heads.isEmpty then continue
    let tails ← pb m j
    for h in heads do
      for t in tails do
        out := out.push (.cat i j h t)
  pure out.toList

/--
The occurrence lists of a duplication over `[i, j)`, sections 8.4 and 8.5.

`done` counts the occurrences already taken and `hasEmpty` says whether one of them was a null match.
The empty-occurrence rule of section 8.5 is the following: a null occurrence is taken only while the
count is below the minimum, and once the minimum is met after a null occurrence, no further occurrence is
taken at all. Beyond that, an occurrence list is complete when it ends exactly at `j` with the minimum met.
-/
def repInstances (sub : Nat → Nat → SM (List PTree)) (min : Nat) (max : Option Nat)
    (i j done : Nat) (hasEmpty : Bool) : Nat → SM (List (List PTree))
  | 0 => pure []
  | fuel + 1 => do
    let mut out : Array (List PTree) := #[]
    if i == j && done ≥ min && (!hasEmpty || done == min) then out := out.push []
    if max == some done then return out.toList
    if hasEmpty && done ≥ min then return out.toList
    for m in [i:j+1] do
      if m == i && done ≥ min then continue
      let heads ← sub i m
      if heads.isEmpty then continue
      let tails ← repInstances sub min max m j (done + 1) (hasEmpty || m == i) fuel
      for h in heads do
        for t in tails do
          out := out.push (h :: t)
    pure out.toList

/--
Every derivation of `e`, whose pre-order id is `id`, over exactly `[i, j)`: the existence relation of
section 4.2 made explicit, together with the empty-occurrence rule of section 8.5.
-/
def parses (ctx : Ctx) : Ere → Nat → Nat → Nat → SM (List PTree)
  | .char c, _, i, j => pure <|
    if j == i + 1 && charAccepts ctx c ctx.subj.chars[i]! then [.leaf i j] else []
  | .any, _, i, j => pure <|
    if j == i + 1 && anyAccepts ctx ctx.subj.chars[i]! then [.leaf i j] else []
  | .bracket b, _, i, j => pure <|
    if j == i + 1 then
      if bracketAcceptsOne ctx b ctx.subj.chars[i]! then [.leaf i j] else []
    else if j > i + 1 && j ≤ ctx.subj.chars.size then
      if bracketAcceptsMulti ctx b (ctx.subj.chars.extract i j).toList then [.leaf i j] else []
    else []
  | .bol, _, i, j => pure (if i == j && atBOL ctx i then [.leaf i j] else [])
  | .eol, _, i, j => pure (if i == j && atEOL ctx i then [.leaf i j] else [])
  | .group _ e, id, i, j => memo id i j do
    pure ((← parses ctx e (id + 1) i j).map (.group i j))
  | .alt a b, id, i, j => memo id i j do
    let left ← parses ctx a (id + 1) i j
    let right ← parses ctx b (id + 1 + a.size) i j
    pure (left.map (.alt i j 0) ++ right.map (.alt i j 1))
  | .cat a b, id, i, j => memo id i j do
    catParses (parses ctx a (id + 1)) (parses ctx b (id + 1 + a.size)) i j
  | .rep min max minimal e, id, i, j => memo id i j do
    if i == j && min == 0 then
      -- A null repetition takes one null occurrence when its operand has one, and no occurrence
      -- otherwise: a null match is preferable to nonparticipation (sections 4.3 and 8.5).
      let subs ← if max == some 0 then pure [] else parses ctx e (id + 1) i i
      if subs.isEmpty then pure [.rep id minimal i j []]
      else pure (subs.map fun s => .rep id minimal i j [s])
    else
      let lists ← repInstances (parses ctx e (id + 1)) min max i j 0 false (min + (j - i) + 2)
      pure (lists.map (.rep id minimal i j))
where
  /-- Look the span up before enumerating it, and record the answer. -/
  memo (id i j : Nat) (compute : SM (List PTree)) : SM (List PTree) := do
    match (← get).table.get? (id, i, j) with
    | some cached => pure cached
    | none =>
      if !(← tick) then pure []
      else
        let out ← compute
        modify fun m => { m with table := m.table.insert (id, i, j) out }
        pure out

/-! ## Selection, section 4.3 -/

mutual

/-- The total span consumed by the repetition node `slot` across the whole derivation. -/
def PTree.slotTotal (slot : Nat) : PTree → Nat
  | .leaf _ _ => 0
  | .group _ _ t => t.slotTotal slot
  | .alt _ _ _ t => t.slotTotal slot
  | .cat _ _ a b => a.slotTotal slot + b.slotTotal slot
  | .rep id _ i j ts => (if id == slot then j - i else 0) + slotTotalList slot ts

def slotTotalList (slot : Nat) : List PTree → Nat
  | [] => 0
  | t :: ts => t.slotTotal slot + slotTotalList slot ts

end

/-- Section 4.3 rule 2: the totals of every shortest-preferring repetition, in pattern order. -/
def totals (slots : List Nat) (t : PTree) : List Nat := slots.map (t.slotTotal ·)

/-- Lexicographic order on the totals: smaller is preferred. -/
def lexLt : List Nat → List Nat → Bool
  | a :: as, b :: bs => a < b || (a == b && lexLt as bs)
  | _, _ => false

/-- Compare two spans the way their node prefers: longest normally, shortest for a minimal repetition. -/
def spanOrd (minimal : Bool) (sa sb : Nat) : Ordering :=
  if minimal then compare sa sb else compare sb sa

mutual

/--
Section 4.3 rules 3 and 4, on two derivations of the same node with equal totals and whole length.
Subpatterns are resolved from left to right in pre-order: a node's own span decides first, then its
children in order. An alternation with equal spans prefers the earlier branch, whose subpatterns come
first. A repetition compares the spans of its occurrences in order, then prefers fewer occurrences.
-/
def cmpTree : PTree → PTree → Ordering
  | .leaf _ _, .leaf _ _ => .eq
  | .group i j ta, .group i' j' tb =>
    match spanOrd false (j - i) (j' - i') with
    | .eq => cmpTree ta tb
    | o => o
  | .alt i j ba ta, .alt i' j' bb tb =>
    match spanOrd false (j - i) (j' - i') with
    | .eq => if ba != bb then compare ba bb else cmpTree ta tb
    | o => o
  | .cat i j a1 a2, .cat i' j' b1 b2 =>
    match spanOrd false (j - i) (j' - i') with
    | .eq =>
      match cmpTree a1 b1 with
      | .eq => cmpTree a2 b2
      | o => o
    | o => o
  | .rep _ minimal i j tas, .rep _ _ i' j' tbs =>
    match spanOrd minimal (j - i) (j' - i') with
    | .eq =>
      match cmpSpans minimal tas tbs with
      | .eq =>
        match compare tas.length tbs.length with
        | .eq => cmpList tas tbs
        | o => o
      | o => o
    | o => o
  | _, _ => .eq

def cmpSpans (minimal : Bool) : List PTree → List PTree → Ordering
  | a :: as, b :: bs =>
    match spanOrd minimal a.span b.span with
    | .eq => cmpSpans minimal as bs
    | o => o
  | _, _ => .eq

def cmpList : List PTree → List PTree → Ordering
  | a :: as, b :: bs =>
    match cmpTree a b with
    | .eq => cmpList as bs
    | o => o
  | _, _ => .eq

end

/--
Section 4.3 at one start position: `a` is preferred over `b` when its shortest-preferring repetitions
consume less (rule 2, the repetition preference), else when the whole match is longer (rule 2), else by the
subpattern order of rules 3 and 4.
-/
def better (slots : List Nat) (a b : PTree) : Bool :=
  let ta := totals slots a
  let tb := totals slots b
  if lexLt ta tb then true
  else if lexLt tb ta then false
  else if a.j != b.j then a.j > b.j
  else cmpTree a b == .lt

/-- The selected derivation among those starting at `start`, if any. -/
def bestAt (ctx : Ctx) (e : Ere) (slots : List Nat) (start : Nat) : SM (Option PTree) := do
  let mut best : Option PTree := none
  for stop in [start:ctx.subj.chars.size + 1] do
    for t in ← parses ctx e 0 start stop do
      match best with
      | none => best := some t
      | some b => if better slots t b then best := some t
  pure best

/-- Section 4.3 rule 1: the earliest start that has a match wins. -/
def select (ctx : Ctx) (e : Ere) : SM (Option PTree) := do
  let slots := e.minimalIds 0
  for start in [0:ctx.subj.chars.size + 1] do
    match ← bestAt ctx e slots start with
    | some t => return some t
    | none => pure ()
  pure none

/-! ## Captures, section 12.7 -/

/--
Walk the pattern and its derivation together. A group reports its last participation: entering a
participation first clears every group nested inside, so a nested group that took no part in this
participation reports nonparticipation even if it took part in an earlier one.
-/
def assign (e : Ere) (t : PTree) (caps : Array (Option (Nat × Nat))) : Array (Option (Nat × Nat)) :=
  match e, t with
  | .group idx e, .group i j t =>
    let caps := e.groupsIn.foldl (fun c g => c.setIfInBounds g none) caps
    assign e t (caps.setIfInBounds idx (some (i, j)))
  | .alt a b, .alt _ _ branch t => if branch == 0 then assign a t caps else assign b t caps
  | .cat a b, .cat _ _ ta tb => assign b tb (assign a ta caps)
  | .rep _ _ _ e, .rep _ _ _ _ ts => assignAll e ts caps
  | _, _ => caps
where
  assignAll (e : Ere) : List PTree → Array (Option (Nat × Nat)) → Array (Option (Nat × Nat))
    | [], caps => caps
    | t :: ts, caps => assignAll e ts (assign e t caps)

/-- The outcome of one execution, section 12.2: no match, or the `nsub + 1` entries of `pmatch` in bytes. -/
inductive Outcome where
  | nomatch
  | matched (pmatch : List (Int × Int))
  deriving Repr, BEq, DecidableEq

/--
`regexec()` under the specification: the selected match and its captures, converted to byte offsets
(section 12.6) and filled per section 12.5. Under `REG_NOSUB` `pmatch` is left alone, and the interface
this model is compared against zero-fills it, so every entry reads `(0, 0)` then.
`none` means the enumeration ran out of budget, which is a limit of the checker and says nothing.
-/
def exec (ctx : Ctx) (e : Ere) (nsub : Nat) (budget : Nat) : Option Outcome :=
  let (sel, memo) := (select ctx e).run { budget }
  if memo.exhausted then none
  else
    match sel with
    | none => some .nomatch
    | some t =>
      if ctx.flags.nosub then some (.matched (List.replicate (nsub + 1) (0, 0)))
      else
        let caps := assign e t ((Array.replicate (nsub + 1) none).set! 0 (some (t.i, t.j)))
        let bytes := caps.toList.map fun
          | some (so, eo) => ((ctx.subj.byteAt[so]! : Int), (ctx.subj.byteAt[eo]! : Int))
          | none => (-1, -1)
        some (.matched bytes)

end Ere
