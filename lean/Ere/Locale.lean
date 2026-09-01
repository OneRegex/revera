/-
The locale adapter of the specification, section 15.1, and the POSIX locale of sections 7.6, 7.7 and 10.2.

The match semantics never reads locale data directly.
It asks a `Locale` value the five questions section 15.1 lists: character decoding is UTF-8 and lives in
`Subject.lean`, and the four remaining questions are the fields below.
-/

namespace Ere

/-- A character is a Unicode scalar value. Subjects and patterns decode from UTF-8 into these. -/
notation "Chr" => Nat

/-- The twelve class names every locale must define, section 7.6. -/
def standardClasses : List String :=
  ["alnum", "alpha", "blank", "cntrl", "digit", "graph",
   "lower", "print", "punct", "space", "upper", "xdigit"]

/--
What the match semantics needs from `LC_CTYPE` and `LC_COLLATE`, section 15.1.

- `classMember name c` answers `[:name:]` for a single character. `hasClass` says whether `name` is a
  class name of the locale at all; an unknown name is not a `class_name` token (section 7.6).
- `toUpper` and `toLower` are the one-character counterpart mappings that the `REG_ICASE` closure of
  section 10.2 is taken over.
- `casePreimages c` lists every character other than `c` whose counterpart is `c`.
  The closure rule quantifies over accepted characters, so the computable form needs the inverse image.
  For the POSIX locale, `posix_casePreimages_complete` below proves the list complete.
- `isCollatingElement seq` says whether `seq` names one collating element (section 7.4).
- `primaryEqual a b` is primary collation equivalence between two collating elements (section 7.5).
- `maxElementLength` bounds the character count of a collating element.
- `posixRanges` is true exactly in the POSIX locale, where section 7.7 defines ranges.
  Everywhere else a range is wholly unspecified and the parser reports it as such.
-/
structure Locale where
  hasClass : String → Bool
  classMember : String → Chr → Bool
  toUpper : Chr → Chr
  toLower : Chr → Chr
  casePreimages : Chr → List Chr
  isCollatingElement : List Chr → Bool
  primaryEqual : List Chr → List Chr → Bool
  maxElementLength : Nat
  posixRanges : Bool

namespace Posix

/-- The required minimum sets of section 7.6, on the ASCII coded set. -/
def upper (c : Chr) : Bool := 65 ≤ c && c ≤ 90
def lower (c : Chr) : Bool := 97 ≤ c && c ≤ 122
def alpha (c : Chr) : Bool := upper c || lower c
def digit (c : Chr) : Bool := 48 ≤ c && c ≤ 57
def alnum (c : Chr) : Bool := alpha c || digit c
def xdigit (c : Chr) : Bool := digit c || (65 ≤ c && c ≤ 70) || (97 ≤ c && c ≤ 102)
def blank (c : Chr) : Bool := c == 32 || c == 9
def space (c : Chr) : Bool := blank c || c == 10 || c == 11 || c == 12 || c == 13
/-- The portable control characters: NUL through US, and DEL. -/
def cntrl (c : Chr) : Bool := c ≤ 31 || c == 127
/-- Printable portable characters other than space or an alphanumeric character. -/
def punct (c : Chr) : Bool := 33 ≤ c && c ≤ 126 && !alnum c
def graph (c : Chr) : Bool := alnum c || punct c
def print (c : Chr) : Bool := graph c || c == 32

def classMember (name : String) (c : Chr) : Bool :=
  match name with
  | "alnum" => alnum c
  | "alpha" => alpha c
  | "blank" => blank c
  | "cntrl" => cntrl c
  | "digit" => digit c
  | "graph" => graph c
  | "lower" => lower c
  | "print" => print c
  | "punct" => punct c
  | "space" => space c
  | "upper" => upper c
  | "xdigit" => xdigit c
  | _ => false

/-- The POSIX locale maps only the portable Latin letters, section 10.2. -/
def toUpper (c : Chr) : Chr := if lower c then c - 32 else c
def toLower (c : Chr) : Chr := if upper c then c + 32 else c

def casePreimages (c : Chr) : List Chr :=
  if upper c then [c + 32] else if lower c then [c - 32] else []

theorem casePreimages_complete (c d : Chr) (h : toUpper d = c ∨ toLower d = c) :
    d = c ∨ d ∈ casePreimages c := by
  simp only [toUpper, toLower, upper, lower, casePreimages] at *
  rcases h with h | h
  · split at h
    · rename_i hd
      simp only [Bool.and_eq_true, decide_eq_true_eq] at hd
      subst h
      have hc : (decide (65 ≤ d - 32) && decide (d - 32 ≤ 90)) = true := by
        simp only [Bool.and_eq_true, decide_eq_true_eq]; omega
      rw [if_pos hc]
      right
      simp only [List.mem_singleton]
      omega
    · left; exact h
  · split at h
    · rename_i hd
      simp only [Bool.and_eq_true, decide_eq_true_eq] at hd
      subst h
      have hc : (decide (65 ≤ d + 32) && decide (d + 32 ≤ 90)) = false := by
        simp only [Bool.and_eq_false_iff, decide_eq_false_iff_not]; omega
      have hl : (decide (97 ≤ d + 32) && decide (d + 32 ≤ 122)) = true := by
        simp only [Bool.and_eq_true, decide_eq_true_eq]; omega
      rw [if_neg (by rw [hc]; decide), if_pos hl]
      right
      simp only [List.mem_singleton]
      omega
    · left; exact h

end Posix

/--
The POSIX locale.
Every character is a one-character collating element and there are no multi-character ones (section 7.4).
No two distinct characters share a primary weight (section 7.5), so `[=x=]` is `[.x.]`.
Ranges follow the coded character set order (section 7.7).
-/
def posixLocale : Locale where
  hasClass name := standardClasses.contains name
  classMember := Posix.classMember
  toUpper := Posix.toUpper
  toLower := Posix.toLower
  casePreimages := Posix.casePreimages
  isCollatingElement seq := seq.length == 1
  primaryEqual a b := a == b
  maxElementLength := 1
  posixRanges := true

end Ere
