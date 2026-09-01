/-
The conformance examples of section 16, checked against the model.

These pin the formalization to the normative examples of the specification. Each one is the required result
of the section 16 tables, and the checker is native evaluation of the model.
-/

import Ere.Semantics

namespace Ere

/-- Run one pattern on one subject in the POSIX locale, with a comfortable work budget. -/
def runStr (pattern subject : String) (flags : CFlags := {}) (eflags : EFlags := {}) :
    Option Outcome :=
  match parsePattern posixLocale flags pattern.toUTF8 with
  | .defined e nsub =>
    match Subject.ofBytes subject.toUTF8 with
    | some subj => exec { loc := posixLocale, flags, eflags, subj } e nsub 1000000
    | none => none
  | _ => none

def classify (pattern : String) (loc : Locale := posixLocale) : ParseResult :=
  parsePattern loc {} pattern.toUTF8

/-! ### 16.1 Selection and grouping -/

example : runStr "b|ab" "ab" = some (.matched [(0, 2)]) := by native_decide
example : runStr "a|ab" "ab" = some (.matched [(0, 2)]) := by native_decide
example : runStr "(a|aa)(a*)" "aaa" = some (.matched [(0, 3), (0, 2), (2, 3)]) := by native_decide
example : runStr "(ab)*" "abab" = some (.matched [(0, 4), (2, 4)]) := by native_decide
example : runStr "(a)?b" "b" = some (.matched [(0, 1), (-1, -1)]) := by native_decide
example : runStr "(a*)b" "b" = some (.matched [(0, 1), (0, 0)]) := by native_decide
example : runStr ".*c" "abc abc" = some (.matched [(0, 7)]) := by native_decide
example : runStr ".*?c" "abc abc" = some (.matched [(0, 3)]) := by native_decide
example : runStr "(.*?).*" "abcdef" = some (.matched [(0, 6), (0, 0)]) := by native_decide
example : runStr ".*" "abc" { minimal := true } = some (.matched [(0, 0)]) := by native_decide
example : runStr ".*?" "abc" { minimal := true } = some (.matched [(0, 3)]) := by native_decide

/-! ### 16.2 Anchors and newline -/

example : runStr "a^" "a" = some .nomatch := by native_decide
example : runStr "$a" "a" = some .nomatch := by native_decide
example : runStr "." "\n" = some (.matched [(0, 1)]) := by native_decide
example : runStr "." "\n" { newline := true } = some .nomatch := by native_decide
example : runStr "[\n]" "\n" { newline := true } = some (.matched [(0, 1)]) := by native_decide
example : runStr "[^a]" "\n" { newline := true } = some .nomatch := by native_decide
example : runStr "^b" "a\nb" { newline := true } { notbol := true } = some (.matched [(2, 3)]) := by
  native_decide
example : runStr "a$" "a\nb" { newline := true } { noteol := true } = some (.matched [(0, 1)]) := by
  native_decide
example : runStr "^b" "b" {} { notbol := true } = some .nomatch := by native_decide

/-! ### 16.3 Brackets and locale -/

example : runStr "[[.a.]]" "a" = some (.matched [(0, 1)]) := by native_decide
example : runStr "[[=a=]]" "a" = some (.matched [(0, 1)]) := by native_decide
example : runStr "[^a]" "A" { icase := true } = some (.matched [(0, 1)]) := by native_decide
example : runStr "[^a]" "a" { icase := true } = some (.matched [(0, 1)]) := by native_decide
example : runStr "[[:digit:]]" "7" = some (.matched [(0, 1)]) := by native_decide
example : runStr "[[:digit:]]" "x" = some .nomatch := by native_decide
example : runStr "[^[:digit:]]" "A" = some (.matched [(0, 1)]) := by native_decide
example : runStr "[^[:digit:]]" "7" = some .nomatch := by native_decide
example : runStr "[a-c]" "b" = some (.matched [(0, 1)]) := by native_decide
example : runStr "[a-c]" "d" = some .nomatch := by native_decide
example : runStr "[]-]" "]" = some (.matched [(0, 1)]) := by native_decide
example : runStr "[]-]" "-" = some (.matched [(0, 1)]) := by native_decide
example : runStr "[\\]" "\\" = some (.matched [(0, 1)]) := by native_decide

/-! ### 16.4 API reporting and section 14 classifications -/

example : (match classify "(a)(b)" with | .defined _ n => n == 2 | _ => false) = true := by native_decide
example : classify "()" = .free := by native_decide
example : classify "a|" = .free := by native_decide
example : classify "a**" = .free := by native_decide
example : classify "a??" != .free := by native_decide
example : classify "a???" = .free := by native_decide
example : classify "[[.xyz.]]" = .invalid := by native_decide
example : classify "a{2,1}" = .free := by native_decide
example : classify "a{256}" = .free := by native_decide
example : classify "a{255}" != .free := by native_decide
example : classify "\\a" = .free := by native_decide
example : classify "a\\" = .free := by native_decide
example : classify "ab)" != .free := by native_decide
example : classify "a{" = .free := by native_decide
example : classify "^*a" = .free := by native_decide
example : classify "[a-m-o]" = .free := by native_decide
example : classify "[.a.]" = .free := by native_decide
example : classify "[:alpha:]" = .free := by native_decide
example : classify "[[:nosuch:]]" = .free := by native_decide
example : classify "[b-a]" = .free := by native_decide
example : classify "[ab" = .free := by native_decide
example : classify "(ab" = .free := by native_decide
example : runStr "ab)" "ab)" = some (.matched [(0, 3)]) := by native_decide
example : runStr "a\\}" "a}" = some (.matched [(0, 2)]) := by native_decide

end Ere
