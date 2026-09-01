/-
The ERE language of sections 5, 6 and 7: the abstract syntax, and a parser that classifies every pattern.

The parser is written from the grammar, not from any engine.
It answers one of three things for a pattern (section 2):

- `defined e nsub`: the pattern is in the portable grammar and denotes `e`, with `nsub` subexpressions.
- `invalid`: the pattern is one of the required errors of section 14.1.
- `free`: the pattern is an undefined spelling (section 14.2) or one of the unspecified choices of
  section 14.3, or it lies outside the domain of the interface altogether. The specification constrains
  nothing about it, so a conformance theorem asks nothing about it either.

`RE_DUP_MAX` is 255 here, the POSIX minimum. A count above it is outside the defined grammar (section 8.4).
-/

import Ere.Locale

namespace Ere

def dupMax : Nat := 255

/-- One member of a bracket list, section 7.1. -/
inductive BracketItem where
  /-- An ordinary character, or a one-character collating symbol `[.c.]`. -/
  | char (c : Chr)
  /-- A range with two one-character endpoints, section 7.7. Only the POSIX locale defines it. -/
  | range (lo hi : Chr)
  /-- A multi-character collating symbol `[.seq.]`, section 7.4. -/
  | elem (seq : List Chr)
  /-- An equivalence class `[=seq=]`, section 7.5. -/
  | equiv (seq : List Chr)
  /-- A character class `[:name:]`, section 7.6. -/
  | cls (name : String)
  deriving Repr, BEq, DecidableEq, Inhabited

structure Bracket where
  negated : Bool
  items : List BracketItem
  deriving Repr, BEq, DecidableEq, Inhabited

/-- The abstract syntax of section 6. Concatenation and alternation are binary and associate to the right. -/
inductive Ere where
  | char (c : Chr)
  | any
  | bracket (b : Bracket)
  | bol
  | eol
  /-- `(e)`, numbered by opening parenthesis from one, section 8.2. -/
  | group (idx : Nat) (e : Ere)
  | cat (a b : Ere)
  | alt (a b : Ere)
  /-- A duplication, section 8.4, with `max = none` for an unbounded one, and its preference (section 8.6). -/
  | rep (min : Nat) (max : Option Nat) (minimal : Bool) (e : Ere)
  deriving Repr, BEq, DecidableEq, Inhabited

inductive ParseResult where
  | defined (e : Ere) (nsub : Nat)
  | invalid
  | free
  deriving Repr, BEq, DecidableEq, Inhabited

/-- Compilation flags, section 11.2. `REG_EXTENDED` is implicit. -/
structure CFlags where
  icase : Bool := false
  newline : Bool := false
  nosub : Bool := false
  minimal : Bool := false
  deriving Repr, BEq, DecidableEq, Inhabited

/-- The flag bits of the engine's interface: 1 ICase, 2 Newline, 4 NoSub, 8 Minimal. -/
def CFlags.ofBits (n : Nat) : CFlags :=
  { icase := n % 2 == 1, newline := (n / 2) % 2 == 1,
    nosub := (n / 4) % 2 == 1, minimal := (n / 8) % 2 == 1 }

/-! ## UTF-8 decoding

The interfaces receive NUL-terminated byte strings and decode them in the locale's encoding, which is UTF-8 here.
A string that is not valid UTF-8, or that holds a NUL, is outside the domain of the interface (section 3.1),
and `decodeUtf8` reports `none` for it. -/

private def cont (b : UInt8) : Bool := 0x80 ≤ b && b ≤ 0xbf

/-- Decode one scalar at `i`, strict: no overlong forms, no surrogates, nothing above U+10FFFF. -/
def decodeOne (bs : ByteArray) (i : Nat) : Option (Chr × Nat) :=
  if h0 : i < bs.size then
    let c0 := bs[i]
    let at1 := fun (k : Nat) => if h : i + k < bs.size then some bs[i + k] else none
    if c0 < 0x80 then some (c0.toNat, 1)
    else if c0 < 0xc2 then none
    else if c0 < 0xe0 then
      match at1 1 with
      | some c1 => if cont c1 then some (((c0.toNat &&& 0x1f) <<< 6) ||| (c1.toNat &&& 0x3f), 2) else none
      | none => none
    else if c0 < 0xf0 then
      match at1 1, at1 2 with
      | some c1, some c2 =>
        if !cont c1 || !cont c2 then none
        else if c0 == 0xe0 && c1 < 0xa0 then none
        else if c0 == 0xed && c1 > 0x9f then none
        else some (((c0.toNat &&& 0x0f) <<< 12) ||| ((c1.toNat &&& 0x3f) <<< 6) ||| (c2.toNat &&& 0x3f), 3)
      | _, _ => none
    else if c0 < 0xf5 then
      match at1 1, at1 2, at1 3 with
      | some c1, some c2, some c3 =>
        if !cont c1 || !cont c2 || !cont c3 then none
        else if c0 == 0xf0 && c1 < 0x90 then none
        else if c0 == 0xf4 && c1 > 0x8f then none
        else some (((c0.toNat &&& 0x07) <<< 18) ||| ((c1.toNat &&& 0x3f) <<< 12) |||
                   ((c2.toNat &&& 0x3f) <<< 6) ||| (c3.toNat &&& 0x3f), 4)
      | _, _, _ => none
    else none
  else none

/--
Decode a whole byte string into characters, with the byte offset of every character boundary.
`byteAt` has one more entry than `chars`, and its last entry is the byte length.
-/
def decodeUtf8 (bs : ByteArray) : Option (Array Chr × Array Nat) :=
  go 0 #[] #[] bs.size
where
  go (i : Nat) (chars : Array Chr) (byteAt : Array Nat) : Nat → Option (Array Chr × Array Nat)
  | 0 => if i == bs.size then some (chars, byteAt.push i) else none
  | fuel + 1 =>
    if i ≥ bs.size then some (chars, byteAt.push i)
    else match decodeOne bs i with
      | some (c, size) => if c == 0 then none else go (i + size) (chars.push c) (byteAt.push i) fuel
      | none => none

/-! ## The parser -/

/-- The parser state: the decoded pattern and a cursor. -/
structure P where
  src : Array Chr
  pos : Nat := 0
  groups : Nat := 0

/-- Failure is one of the two non-defined classifications. -/
inductive Fail where
  | invalid
  | free

abbrev PM := StateT P (Except Fail)

private def peek (k : Nat := 0) : PM (Option Chr) := do
  let p ← get
  pure p.src[p.pos + k]?

private def eof : PM Bool := do
  let p ← get
  pure (p.pos ≥ p.src.size)

private def advance (k : Nat := 1) : PM Unit :=
  modify fun p => { p with pos := p.pos + k }

private def next : PM Chr := do
  match ← peek with
  | some c => advance; pure c
  | none => throw .free

private def isDigit (c : Chr) : Bool := 48 ≤ c && c ≤ 57

/-- The characters that are special outside a bracket expression, section 5.2. -/
def specials : List Chr := "^.[$()|*+?{\\".toList.map Char.toNat

/-- The portable quoted characters, section 5.4: the specials plus `]` and `}`. -/
def quotable : List Chr := specials ++ [93, 125]

/-- The character codes used below, by name. -/
private def cLParen : Chr := 40
private def cRParen : Chr := 41
private def cStar : Chr := 42
private def cPlus : Chr := 43
private def cComma : Chr := 44
private def cDot : Chr := 46
private def cColon : Chr := 58
private def cEq : Chr := 61
private def cQuestion : Chr := 63
private def cLBracket : Chr := 91
private def cBackslash : Chr := 92
private def cRBracket : Chr := 93
private def cCaret : Chr := 94
private def cLBrace : Chr := 123
private def cBar : Chr := 124
private def cRBrace : Chr := 125
private def cDollar : Chr := 36
private def cMinus : Chr := 45

private def isDup (c : Chr) : Bool := c == cStar || c == cPlus || c == cQuestion || c == cLBrace

/-- Find the first occurrence of the two-character closer `a b` at or after `from`, returning its index. -/
private def findCloser (src : Array Chr) (from_ : Nat) (a b : Chr) : Option Nat :=
  go from_ (src.size + 1)
where
  go (i : Nat) : Nat → Option Nat
  | 0 => none
  | fuel + 1 =>
    if i + 1 < src.size then
      if src[i]! == a && src[i + 1]! == b then some i else go (i + 1) fuel
    else none

/--
Scan the content of `[. .]`, `[= =]` or `[: :]` from the cursor at `[`.
The content ends at the first closer. Empty content or a missing closer leaves the grammar, section 14.2.
-/
private def scanInner (mark : Chr) : PM (List Chr) := do
  let p ← get
  match findCloser p.src (p.pos + 2) mark cRBracket with
  | none => throw .free
  | some endIdx =>
    if endIdx == p.pos + 2 then throw .free
    let content := (p.src.extract (p.pos + 2) endIdx).toList
    set { p with pos := endIdx + 2 }
    pure content

/-- A class name is one to fourteen portable filename alphanumerics, not starting with a digit (section 7.6). -/
private def className (loc : Locale) (seq : List Chr) : PM String := do
  let s := String.ofList (seq.map Char.ofNat)
  if !loc.hasClass s then throw .free
  pure s

/-- One list member of a bracket expression that is not a range, sections 7.2, 7.4, 7.5 and 7.6. -/
private def bracketItem (loc : Locale) : PM BracketItem := do
  match ← peek, ← peek 1 with
  | some c, some inner =>
    if c == cLBracket && inner == cDot then
      let seq ← scanInner cDot
      -- Section 14.1: a collating symbol must name a collating element, else the RE is invalid.
      if !loc.isCollatingElement seq then throw .invalid
      match seq with
      | [x] => pure (.char x)
      | _ => pure (.elem seq)
    else if c == cLBracket && inner == cEq then
      let seq ← scanInner cEq
      -- Section 7.5: contents that are not a collating element do not form an equivalence class.
      if !loc.isCollatingElement seq then throw .free
      pure (.equiv seq)
    else if c == cLBracket && inner == cColon then
      let seq ← scanInner cColon
      pure (.cls (← className loc seq))
    else
      pure (.char (← next))
  | some _, none => pure (.char (← next))
  | none, _ => throw .free

/--
The delimiter-shaped exception of section 7.9: a list of at least three characters whose first and last are the
same one of `.`, `=` or `:`. The result is unspecified, so the pattern is free.
-/
private def delimiterShaped (src : Array Chr) (listStart : Nat) : Bool :=
  -- The list ends at the first `]` that is not first in the list.
  let rec go (i : Nat) : Nat → Option Nat
    | 0 => none
    | fuel + 1 =>
      if i < src.size then
        if src[i]! == cRBracket && i > listStart then some i else go (i + 1) fuel
      else none
  match go listStart (src.size + 1), src[listStart]? with
  | some endIdx, some first =>
    endIdx ≥ listStart + 3 && (first == cDot || first == cEq || first == cColon) &&
      src[endIdx - 1]! == first
  | _, _ => false

/--
Parse a bracket expression from the cursor at `[`, section 7.
Range detection follows section 7.8: `-` is literal when first, last, or the ending point of a range.
-/
private def bracket (loc : Locale) : PM Bracket := do
  advance
  let negated ← do
    match ← peek with
    | some c => if c == cCaret then advance; pure true else pure false
    | none => throw .free
  let listStart := (← get).pos
  let mut items : Array BracketItem := #[]
  let mut first := true
  let mut fuel := (← get).src.size + 2
  while fuel > 0 do
    fuel := fuel - 1
    match ← peek with
    | none => throw .free
    | some c =>
      if c == cRBracket && !first then
        advance
        if delimiterShaped (← get).src listStart then throw .free
        return { negated, items := items.toList }
      first := false
      let item ← bracketItem loc
      -- A `-` that is not last in the list is the range separator, section 7.8.
      let sep ← peek
      let after ← peek 1
      if sep == some cMinus && after.isSome && after != some cRBracket then
        advance
        let endItem ← bracketItem loc
        match item, endItem with
        | .char lo, .char hi =>
          -- Section 7.7: only the POSIX locale defines ranges, and an empty set is an unspecified choice.
          if !loc.posixRanges then throw .free
          if hi < lo then throw .free
          items := items.push (.range lo hi)
        | _, _ => throw .free
        -- Section 7.7: sharing an ending point with the next starting point is undefined.
        if (← peek) == some cMinus && (← peek 1) != some cRBracket && (← peek 1).isSome then throw .free
      else
        items := items.push item
  throw .free

/-- One interval `{m}`, `{m,}` or `{m,n}` from the cursor at `{`, section 8.4. -/
private def interval : PM (Nat × Option Nat) := do
  advance
  let count : PM Nat := do
    let mut value := 0
    let mut any := false
    let mut fuel := (← get).src.size + 1
    while fuel > 0 do
      fuel := fuel - 1
      match ← peek with
      | some c =>
        if isDigit c then
          advance
          value := value * 10 + (c - 48)
          any := true
          -- Section 8.4: a count over RE_DUP_MAX is outside the grammar.
          if value > dupMax then throw .free
        else break
      | none => break
    if !any then throw .free
    pure value
  let lo ← count
  match ← peek with
  | some c =>
    if c == cRBrace then advance; pure (lo, some lo)
    else if c == cComma then
      advance
      if (← peek) == some cRBrace then advance; pure (lo, none)
      else
        let hi ← count
        if (← peek) != some cRBrace then throw .free
        advance
        if hi < lo then throw .free
        pure (lo, some hi)
    else throw .free
  | none => throw .free

/-- An optional duplication with its repetition modifier, sections 8.4 and 8.6. -/
private def duplication (flags : CFlags) (operand : Ere) : PM Ere := do
  match ← peek with
  | none => pure operand
  | some c =>
    if !isDup c then pure operand
    else
      let (lo, hi) ←
        if c == cStar then advance; pure (0, none)
        else if c == cPlus then advance; pure (1, none)
        else if c == cQuestion then advance; pure (0, some 1)
        else interval
      let mut minimal := flags.minimal
      if (← peek) == some cQuestion then advance; minimal := !minimal
      -- Section 8.6: any further adjacent duplication symbol is undefined.
      match ← peek with
      | some d => if isDup d then throw .free
      | none => pure ()
      pure (.rep lo hi minimal operand)

/-- Right-nested concatenation of a nonempty list. -/
def catAll : List Ere → Ere
  | [] => .any
  | [e] => e
  | e :: es => .cat e (catAll es)

/-!
The three grammar functions recurse through groups. Each call passes a smaller fuel, so they are total.
The fuel starts above three times the pattern length, and nesting consumes at least one character per
level, so it never runs out on a real pattern.
-/

mutual

/-- `ere = branch { "|" branch }`. -/
private def alternation (loc : Locale) (flags : CFlags) (inGroup : Bool) : Nat → PM Ere
  | 0 => throw .free
  | fuel + 1 => do
    let first ← branch loc flags inGroup fuel
    if (← peek) == some cBar then
      advance
      let rest ← alternation loc flags inGroup fuel
      pure (.alt first rest)
    else pure first

/-- `branch = expression { expression }`, nonempty (section 6). -/
private def branch (loc : Locale) (flags : CFlags) (inGroup : Bool) : Nat → PM Ere
  | 0 => throw .free
  | fuel + 1 => do
    let mut exprs : Array Ere := #[]
    let mut going := true
    let mut steps := (← get).src.size + 1
    while going && steps > 0 do
      steps := steps - 1
      match ← peek with
      | none => going := false
      | some c =>
        if c == cBar || (c == cRParen && inGroup) then going := false
        else exprs := exprs.push (← expression loc flags fuel)
    if exprs.isEmpty then throw .free
    pure (catAll exprs.toList)

/-- `expression = anchor | primary [duplication [repetition_modifier]]`. -/
private def expression (loc : Locale) (flags : CFlags) : Nat → PM Ere
  | 0 => throw .free
  | fuel + 1 => do
    let some c ← peek | throw .free
    if c == cCaret || c == cDollar then
      advance
      -- Section 14.2: a duplication symbol right after an anchor is undefined.
      match ← peek with
      | some d => if isDup d then throw .free
      | none => pure ()
      pure (if c == cCaret then .bol else .eol)
    else if isDup c then throw .free
    else
      let primary ←
        if c == cLParen then do
          advance
          modify fun p => { p with groups := p.groups + 1 }
          let idx := (← get).groups
          let inner ← alternation loc flags true fuel
          if (← peek) != some cRParen then throw .free
          advance
          pure (Ere.group idx inner)
        else if c == cLBracket then do
          pure (Ere.bracket (← bracket loc))
        else if c == cBackslash then do
          advance
          match ← peek with
          | none => throw .free
          | some q => if quotable.contains q then advance; pure (Ere.char q) else throw .free
        else if c == cDot then do
          advance
          pure Ere.any
        else do
          advance
          pure (Ere.char c)
      duplication flags primary

end

/--
Classify and parse one pattern in a locale, under compilation flags.
The pattern is a byte string; one that is not valid UTF-8 or that holds a NUL is outside the interface domain.
-/
def parsePattern (loc : Locale) (flags : CFlags) (pattern : ByteArray) : ParseResult :=
  match decodeUtf8 pattern with
  | none => .free
  | some (chars, _) =>
    match (alternation loc flags false (3 * chars.size + 8)).run { src := chars } with
    | .ok (e, p) => if p.pos == chars.size then .defined e p.groups else .free
    | .error .invalid => .invalid
    | .error .free => .free

end Ere
