# POSIX.1-2024 Issue 8 extended regular expressions

Status: clean-room, implementation-oriented specification, audited against the
published Issue 8 text on 2026-08-24.

## 1. Purpose, authority, and scope

This document specifies the POSIX.1-2024 Issue 8 Extended Regular Expression
(ERE) language and the POSIX C interface that compiles, executes, and reports
matches for that language. It is written so that an implementer does not need
to infer behavior from examples or from a particular existing regex engine.

The normative baseline is the published Open Group Base Specifications Issue 8,
IEEE Std 1003.1-2024. The primary sources are:

- [XBD Chapter 9, Regular Expressions][xbd-9], including the common bracket
  expression rules and the normative grammar;
- [XBD Chapter 4.1, Case-Insensitive Comparisons][xbd-4];
- [XBD Chapter 7, Locale][xbd-7], for `LC_CTYPE`, `LC_COLLATE`, character
  classes, collating elements, and equivalence classes;
- [XBD `<regex.h>`][regex-h] and [XBD `<limits.h>`][limits-h];
- [XSH `regcomp()`, `regexec()`, `regerror()`, and `regfree()`][regcomp];
- [XBD Rationale A.9][xrat-a9], used only to disambiguate and explain the
  normative requirements.

The prose below is a paraphrase and synthesis. If it conflicts with the
published standard, the published standard controls. The POSIX grammar itself
states that it takes priority over the surrounding descriptive text.

This document covers:

- the ERE pattern language;
- the bracket-expression language shared by BREs and EREs;
- locale-sensitive matching;
- leftmost-longest and minimal-repetition match selection;
- parenthesized-subexpression reporting;
- all ERE-relevant requirements of the POSIX `<regex.h>` API; and
- required errors and the boundaries of undefined or unspecified behavior.

It does not specify Basic Regular Expressions, shell pathname patterns, or the
utility-specific exceptions made by tools such as `grep`, `sed`, and `awk`.
Those consumers may impose extra rules. The interface profile here is the
`regcomp()` family, where newline is ordinary unless `REG_NEWLINE` is selected.

## 2. Requirement vocabulary and conformance domains

The following classifications are part of the specification and must remain
distinct in an implementation and its tests.

### 2.1 Required behavior

For a syntactically valid ERE, supported locale, valid API arguments, and
available resources, every rule identified as required in this document is a
conformance requirement. An implementation may add features, but those
features must not change the result for an ERE in this defined domain unless
the application explicitly selects an extension.

### 2.2 Invalid construct

POSIX uses *invalid* for selected errors. An invalid RE must cause the consumer
to report an error. For `regcomp()`, that means a nonzero return. `REG_BADPAT`
is always an acceptable compilation-error code; a more specific standard code
may be returned when applicable.

The principal ERE case explicitly designated invalid is a `[.element.]` whose
contents are not a collating element in the current locale. Some bracket-range
cases may also be treated as invalid where POSIX gives that as one of the
permitted outcomes.

### 2.3 Undefined pattern result

Many strings lie outside the portable ERE language without being designated
invalid. POSIX says their results are undefined. For such a pattern, an
implementation may report an error, treat some characters literally, or give
the spelling an extension meaning. A strictly conforming application cannot
use it. This permission does not allow an extension to alter valid EREs.

Examples include an empty branch, a backslash before an ERE ordinary
character, a left brace that does not begin a valid interval, and adjacent
duplication symbols other than the standardized repetition modifier.

Within the `regcomp()` interface, every `pattern` argument is a NUL-terminated
string. The standard's informative rationale explains that an undefined
pattern spelling should lead to an unspecified compilation interpretation,
not process termination or an invalid memory access.

### 2.4 Unspecified choice

For some valid constructs POSIX deliberately permits a set of results. The
implementation may choose any allowed result and need not document the choice
unless a general POSIX rule requires documentation. Examples include the
meaning of ranges outside the POSIX locale and whether an ordinary bracket
list recognizes implicit multi-character collating elements.

### 2.5 Implementation extension

Additional syntax and flags are permitted, particularly in an undefined area.
Names of added `<regex.h>` constants may begin with `REG_`. A mode that changes
defined ERE behavior is outside this specification and must be separately
selected; it cannot silently replace the POSIX mode.

## 3. Data model

### 3.1 Pattern and subject

An ERE operates on a text string: a sequence of zero or more characters and an
end-of-string delimiter. The `regcomp()` and `regexec()` interfaces receive
NUL-terminated `char` strings. They cannot represent a NUL character inside the
pattern or subject; the first NUL terminates the respective string.

The logical matching unit is a character, not a byte. In a locale with a
multibyte encoding, one character can occupy multiple bytes. A bracket
expression can additionally match one multi-character collating element, which
consumes all characters in that element.

Matching compares encoded characters, not glyph appearance. Canonically or
visually equivalent encodings are not normalized. They match only when the
pattern, case mappings, or an equivalence class makes them equivalent.

### 3.2 Current locale

Compilation and execution use the current locale, specifically:

- `LC_CTYPE` for character decoding, named character classes, and case
  mappings; and
- `LC_COLLATE` for collating elements, collation order, range behavior in the
  POSIX locale, and primary equivalence classes.

If the locale in effect during `regexec()` differs from the locale used by the
successful `regcomp()`, the match result is undefined. A compiled expression
may therefore snapshot locale data or retain whatever locale-dependent form is
convenient, but it must behave as specified while the locale remains the same.

The `POSIX` and `C` locales denote the same locale on a conforming system.

### 3.3 Newline

In the base ERE language, newline is an ordinary character. A literal newline
can appear in the pattern accepted by `regcomp()`. Dot and a non-matching list
can match newline. `REG_NEWLINE` changes these points as specified in Section
12.3.

Utility specifications can prohibit newline in patterns or subjects. Such a
restriction belongs to the utility, not the base ERE language.

### 3.4 Minimum capacity

An implementation must support every otherwise-supported RE whose pattern is
no more than 256 bytes long. This is a minimum guaranteed capacity, not a
maximum pattern length.

The `RE_DUP_MAX` value supplied by `<limits.h>` is at least
`_POSIX_RE_DUP_MAX`, whose value is 255. A specific running instance may
advertise a larger actual limit through `sysconf()`. Interval bounds from zero
through the effective `{RE_DUP_MAX}` must be representable and supported.

Likewise, the `<limits.h>` `CHARCLASS_NAME_MAX` value is at least
`_POSIX2_CHARCLASS_NAME_MAX`, whose value is 14, and the running instance may
support longer locale-defined class names.

## 4. Terms and abstract matching model

### 4.1 Terms

- An **entire ERE** is the complete pattern supplied by its consumer.
- A **character-matching atom** matches one character or, only through a
  bracket expression, one collating element.
- A **subexpression** is an ERE enclosed in unescaped `(` and `)`. Every such
  pair captures and is numbered; ERE has no noncapturing group.
- A **branch** is a nonempty concatenation of ERE expressions.
- A **null match** consumes zero characters. It differs from a subexpression
  that did not participate in the selected match.
- **Leftmost** means closest to the beginning of the subject.
- A **duplication symbol** is `*`, `+`, `?`, or a valid interval.
- A duplication is **longest-preferring** by default and
  **shortest-preferring** when its preference is inverted as described in
  Section 8.6.

### 4.2 Existence of a match

Ignoring selection among alternatives for the moment, an ERE matches a subject
substring if its syntax can consume exactly that substring as follows:

- a character-matching atom consumes a character or collating element in its
  denoted set;
- concatenation consumes adjacent substrings in component order;
- alternation consumes a substring accepted by either branch;
- grouping has the language of its enclosed ERE;
- anchors consume no characters and assert a permitted boundary; and
- duplication concatenates an allowed number of occurrences of its operand.

Matching is a search, not an implicit whole-string comparison. Anchors are
needed when the match must begin or end at the subject boundary.

### 4.3 Match selection

The implementation must not use "first successful backtracking path" as its
observable rule. It must produce the POSIX-selected match:

1. Prefer a match whose beginning is earlier in the subject over every match
   beginning later. A shorter earlier match beats a longer later match.
2. At that beginning, normally choose the longest possible whole match. For
   each shortest-preferring repetition, give its shortest possible matching
   prefix priority over that ordinary longest choice, while still requiring
   the remainder of the ERE to match.
3. Consistently with the selected whole match, resolve subpatterns from left to
   right. A normal subpattern takes its longest possible compatible match. A
   shortest-preferring repetition takes its shortest possible compatible match,
   including zero occurrences; a longest-preferring repetition takes its
   longest possible compatible match.
4. Choices for later subpatterns cannot reduce a choice already fixed for an
   earlier, higher-priority subpattern. All choices must still permit the
   remainder of the entire ERE to match.

Thus preference is constrained by the successful enclosing match. It is not a
possessive operation. For example, `.*c` on `abc abc` reaches the final `c`,
while `.*?c` stops at the first `c`. In `(.*?).*` on `abcdef`, the first group
captures the empty string and the second repetition consumes the rest, so the
whole match remains `abcdef`.

Length is the number of subject characters consumed. A multi-character
collating element counts by the number of characters it consumes, not as one
unit for length comparison. A null match is preferable to nonparticipation when
the comparison otherwise reaches that subpattern.

An implementation strategy may enumerate parses, use tagged automata, or use
another algorithm. It is conformant only if these observable choices are the
same.

## 5. Lexical rules

### 5.1 Longest token

At each pattern position, recognize the longest token or delimiter available in
the applicable lexical context, except where a rule below states otherwise.
Bracket expressions have their own context-sensitive lexer.

Decimal digits form an interval count only where the interval grammar expects a
count. Elsewhere an unescaped digit is an ordinary character. A count is valid
only in the inclusive range zero through `RE_DUP_MAX`.

### 5.2 ERE special characters

Outside a bracket expression, these characters are special in the contexts
described by this document:

```text
^ . [ $ ( ) | * + ? { \
```

`)` is special only when it closes a preceding unmatched `(`. Otherwise it is
an ordinary character. `^` and `$` are anchors everywhere outside brackets,
even in positions where the resulting ERE can never match.

### 5.3 Ordinary characters

Outside brackets, every supported character other than a special character is
ordinary and matches itself. A special character used outside the context that
gives it a special function, or preceded by an unescaped backslash, matches
itself subject to the escape rules below. The explicitly undefined operator
placements in Section 14.2 override this general rule.

### 5.4 Portable quoted characters

Outside brackets, each of the following escape sequences matches the character
after the backslash literally:

```text
\^  \.  \[  \]  \$  \(  \)  \|
\*  \+  \?  \{  \}  \\
```

The explicit `\]` and `\}` cases are required even where unescaped `]` or `}`
would otherwise be ordinary. A trailing backslash is outside the defined
grammar; if compilation rejects it, `REG_EESCAPE` is the specific error.

Outside brackets, a backslash before any other ordinary character has undefined
meaning. In particular, POSIX ERE defines no backreferences, control escapes,
word-boundary escapes, shorthand classes, hexadecimal escapes, or quoting
escapes.

Inside a bracket expression, backslash loses its ERE escape function and is an
ordinary list character. Section 7 gives the complete bracket rules.

## 6. Defined ERE grammar

The following EBNF describes the fully defined, portable ERE subset. It folds
the standardized repetition modifier into the older yacc-style POSIX grammar
and excludes spellings that POSIX explicitly assigns undefined results.

```ebnf
ere                   = branch, { "|", branch } ;
branch                = expression, { expression } ;

expression            = anchor
                      | primary, [ duplication, [ repetition_modifier ] ] ;

anchor                = "^" | "$" ;

primary               = one_character_or_collating_element
                      | "(", ere, ")" ;

one_character_or_collating_element
                      = ordinary_character
                      | quoted_character
                      | "."
                      | bracket_expression ;

duplication           = "*"
                      | "+"
                      | "?"
                      | interval ;

repetition_modifier   = "?" ;

interval              = "{", count, "}"
                      | "{", count, ",}"
                      | "{", count, ",", count, "}" ;
```

Additional constraints are normative:

- `ere` and every `branch` are nonempty. Therefore an empty pattern, `()`, and
  a missing alternative beside `|` are outside the defined grammar.
- In `{m,n}`, both counts are decimal integers, `0 <= m <= n <= RE_DUP_MAX`.
  In `{m}` and `{m,}`, `0 <= m <= RE_DUP_MAX`.
- A duplication can follow only a character-matching primary or a parenthesized
  ERE. It cannot follow an anchor.
- At most one duplication follows a primary. One additional `?` is valid only
  as that duplication's repetition modifier.
- Parentheses may nest without a language-specified depth limit, subject to
  resources and the 256-byte minimum-capacity rule.

The grammar establishes precedence and associativity as detailed in Section 9.

## 7. Bracket expressions

Bracket expressions are locale-sensitive character or collating-element sets.
The same bracket rules apply to BRE and ERE; this section gives the complete
rules needed by an ERE implementation.

### 7.1 Overall form

```ebnf
bracket_expression = "[", matching_list, "]"
                   | "[^", nonempty_list, "]" ;

matching_list      = nonempty_list ;
```

The internal list grammar, expressed with contextual tokens, is:

```ebnf
nonempty_list       = follow_list, [ literal_trailing_hyphen ] ;
follow_list         = expression_term, { expression_term } ;
expression_term     = single_expression | range_expression ;

single_expression   = end_range
                    | character_class
                    | equivalence_class ;

range_expression    = start_range, end_range
                    | start_range, literal_ending_hyphen ;
start_range         = end_range, range_hyphen ;

end_range           = nonmetadata_single_character_collating_element
                    | collating_symbol ;

collating_symbol    = "[.", collating_symbol_content, ".]" ;
collating_symbol_content
                     = nonmetadata_single_character_collating_element
                     | multi_character_collating_element
                     | metadata_character ;
equivalence_class   = "[=", equivalence_class_content, "=]" ;
equivalence_class_content
                     = nonmetadata_single_character_collating_element
                     | multi_character_collating_element ;
character_class     = "[:", current_locale_class_name, ":]" ;
```

`range_hyphen`, `literal_ending_hyphen`, and `literal_trailing_hyphen` are the
same `-` character classified by position. A `metadata_character` is a
one-character collating element classified as POSIX's contextual `META_CHAR`
token under Sections 7.2 and 7.8. A `collating_symbol` accepts that token, but
an `equivalence_class` does not. Both constructs accept non-metadata
one-character elements and locale-defined multi-character elements. An
`end_range` deliberately excludes character and equivalence classes.

The initial `^`, when present immediately after `[`, negates the corresponding
positive list. It is not a list member. In every other list position, `^` is an
ordinary member.

The list must contain at least one expression. Its members can be:

- an ordinary single-character collating element;
- an explicit collating symbol `[.element.]`;
- an equivalence class `[=element=]`;
- a character class `[:name:]`; or
- a range with two permitted endpoints.

The delimiters in the last three forms occur inside the outer brackets. For
example, the complete ERE matching the `alpha` class is `[[:alpha:]]`, and the
complete ERE containing the collating symbol `ch` is `[[.ch.]]`.

### 7.2 Context-sensitive characters

Inside a bracket expression:

- `. ( * + ? { | $ [ \` have no ordinary ERE special meaning;
- `^` has only the initial negation meaning described above;
- backslash is an ordinary character, not an escape introducer;
- the two-character openers `[.`, `[=`, and `[:` are special and must be
  processed as the start of a collating symbol, equivalence class, or character
  class, respectively, subject to the delimiter-shaped ambiguity in Section
  7.9; and
- `]` closes the bracket expression except in the special cases below.

A `]` is an ordinary list member when it is the first list character after `[`
or after the initial `[^`. It also retains its data meaning inside `[. .]` and
acts as the required final bracket in `. ]`, `= ]`, or `: ]` delimiters (with
no intervening space). Otherwise it terminates the outer bracket expression.
Thus `[]a]` matches `]` or `a`, and `[^]a]` matches characters other than `]`
and `a`.

### 7.3 Matching and non-matching lists

A matching list accepts a single character if at least one member expression
accepts it. An ordinary list character accepts only itself. Duplicate members
do not change the set.

Whether a matching list also recognizes a multi-character collating element
implicitly matched by one of its members is unspecified. An implementation may
restrict ordinary lists and character-class expressions to single characters,
or may recognize applicable locale-defined multi-character elements. Explicit
`[.element.]` syntax is not optional and must recognize the named element.

A non-matching list is the logical inverse of the corresponding matching list
with its initial `^` removed. It accepts any single character the positive list
does not accept. Whether it recognizes a multi-character collating element not
accepted by the positive list is unspecified.

That inverse defines case-sensitive membership. Under `REG_ICASE`, apply the
closure rule in Section 10.2 after taking this inverse; do not case-expand the
positive list before taking its inverse.

`REG_NEWLINE` adds one exception: no non-matching list may match newline when
that flag is in effect, even if newline is absent from its positive list.

### 7.4 Collating symbols

`[.element.]` denotes one collating element in the current `LC_COLLATE` locale.
The element can be one character or a locale-defined sequence of two or more
characters. For example, if `ch` is defined as one collating element,
`[[.ch.]]` consumes both characters as one bracket-expression match, whereas
`[ch]` ordinarily consumes either `c` or `h`.

The character sequence between `[.` and `.]` must name an actual collating
element in the current locale. Otherwise the bracket expression is invalid and
compilation must fail. A collating symbol is recognized only inside a bracket
expression.

The RE term *collating symbol* means this bracket-delimited spelling of an
element's character sequence. It must not be confused with an `LC_COLLATE`
locale-definition `collating-symbol`, which can name an abstract weight
position and need not represent any matchable element.

The POSIX locale contains no multi-character collating elements. Every
character in that locale is nevertheless a one-character collating element, so
forms such as `[[.a.]]` are valid and required.

### 7.5 Equivalence classes

`[=element=]` denotes every collating element with the same primary collation
weight as the named element in the current locale. Only primary equivalence is
used; secondary and later weights do not restrict this set.

Any member of the class may name it. If `a`, `à`, and `â` have one primary
weight, `[[=a=]]`, `[[=à=]]`, and `[[=â=]]` denote the same set. Elements whose
primary weight is `IGNORE` collectively form one equivalence class.

If the named collating element belongs to no multi-element equivalence class,
the expression behaves as the collating symbol for that element; it still
matches the element itself. Contents that are not a collating element do not
form the grammar's `equivalence_class`; an implementation that rejects them can
use `REG_ECOLLATE`.

### 7.6 Character classes

`[:name:]` denotes the union of:

1. all single characters assigned to `name` by `LC_CTYPE`; and
2. an implementation-chosen, unspecified set of multi-character collating
   elements.

Every character class defined by the current locale must be recognized. These
twelve names must exist in every locale:

```text
alnum  alpha  blank  cntrl  digit  graph
lower  print  punct  space  upper  xdigit
```

Locale-defined extra class names must also work. Such a name consists of one to
`CHARCLASS_NAME_MAX` bytes from the portable filename alphanumeric set, cannot
begin with a digit, and is defined by the locale. A class name unknown in the
current locale is not a `class_name` token in the POSIX grammar. The pattern is
therefore outside the defined language; an implementation that rejects it can
use `REG_ECTYPE`.

In every locale, `digit` contains only the portable `0` through `9`, and
`xdigit` contains only those digits plus `A` through `F` and `a` through `f`.
`alnum` includes exactly the locale's `alpha` and `digit` members. The portable
uppercase and lowercase Latin letters, the six portable whitespace characters,
and space/tab as `blank` are mandatory members of their corresponding classes;
the remaining membership comes from `LC_CTYPE` subject to that category's
required class relationships.

In the POSIX locale, the required minimum sets are:

| Class    | Members                                                                     |
| -------- | --------------------------------------------------------------------------- |
| `upper`  | `A` through `Z`                                                             |
| `lower`  | `a` through `z`                                                             |
| `alpha`  | `upper` union `lower`                                                       |
| `digit`  | `0` through `9`                                                             |
| `alnum`  | `alpha` union `digit`                                                       |
| `xdigit` | `0`-`9`, `A`-`F`, and `a`-`f`                                               |
| `blank`  | space and tab                                                               |
| `space`  | space, form feed, newline, carriage return, tab, and vertical tab           |
| `cntrl`  | the portable control characters, including NUL through US and DEL           |
| `punct`  | printable portable characters other than space or an alphanumeric character |
| `graph`  | `alnum` union `punct`                                                       |
| `print`  | `graph` union space                                                         |

The POSIX locale may make implementation-chosen base additions to `cntrl` and
`punct`, but not to the other ten classes; the required relationships among
classes still propagate any resulting membership into `graph` or `print` where
applicable. This allowance does not make embedded NUL matchable through the
`regcomp()` interface; NUL still terminates its strings.

### 7.7 Ranges

A range has a starting point and ending point separated by `-`. Each point must
be a one-character collating element or an explicit collating symbol. A
character class cannot be an endpoint. Using an equivalence class as either
endpoint has unspecified results.

In the POSIX locale, a range denotes every collating element from the starting
point through the ending point in the locale's collation sequence, inclusive.
The POSIX locale has no multi-character collating elements and orders the
portable character set in the order used by the ASCII coded set; additional
characters follow the portable set with unique primary weights. If the listed
portable characters have ASCII encoding, those additional characters are
ordered by ascending coded character set value. Otherwise, their relative
order is unspecified. Consequently, `[a-z]` denotes the 26 portable lowercase
letters in that locale.

For range construction, the relevant portable sequence is: control characters
from NUL through US; space; `!` through `/`; `0` through `9`; `:` through `@`;
`A` through `Z`; left square bracket through grave accent; `a` through `z`;
`{` through `~`; and DEL. This describes collation order, not a requirement
that the locale encode characters with ASCII byte values.

Outside the POSIX locale, the entire behavior of a range is unspecified: an
implementation may reject it or choose its matched set. A conforming
application that needs non-POSIX-locale portability uses character classes,
explicit list members, collating symbols, or equivalence classes instead.

If a range would denote an empty set, the implementation may either make it a
valid range matching nothing or treat the bracket expression as invalid.
Using one range's ending point simultaneously as the next range's starting
point, as in `[a-m-o]`, has undefined results.

### 7.8 Literal hyphen

`-` is literal when it is:

- first in the list, after the initial `^` if any;
- last in the list; or
- being recognized as a range's ending point.

Otherwise it is the range separator. To use `-` unambiguously as the starting
point of a noninitial range, use the inner collating-symbol form `[.-.]` within
the outer brackets; for example, `[[.-.]-0]`. If a list needs both `]` and a
literal `-`, portable spelling places `]` first and `-` last, after an initial
`^` if present.

### 7.9 Ambiguous delimiter-shaped lists

If a bracket expression has at least three list elements and its first and last
elements are both `.`, both `=`, or both `:`, it is unspecified whether the
implementation treats the construct as the corresponding collating symbol,
equivalence class, or character class; as an ordinary matching list; or as an
invalid bracket expression. This narrow exception applies where the outer
brackets can be confused with missing inner delimiters. Strictly conforming
patterns avoid these spellings.

## 8. Atoms, grouping, and duplication

### 8.1 Ordinary atom and dot

An ordinary character or portable quoted character consumes the same character
from the subject, modified by `REG_ICASE` if selected.

Outside a bracket expression, `.` consumes any one supported character except
NUL. With `REG_NEWLINE`, it also excludes newline.

### 8.2 Grouping and captures

`(ere)` has the same matching language as `ere` and creates a capturing
subexpression. Groups may nest. Number groups by the order of their opening
parentheses, starting at one. The whole ERE is reported as match zero but is not
included in `re_nsub`.

There is no valid empty group because the enclosed `ere` must contain a branch,
and every branch must contain an expression. There are no ERE backreferences.

### 8.3 Concatenation

Adjacent expressions form a concatenation and consume adjacent subject
substrings in the same order. Concatenation itself has no separator. Each
component's match choice must be compatible with successful matching of every
following component.

### 8.4 Duplication counts

For operand `E`:

| Form     | Permitted consecutive occurrences of `E` |
| -------- | ---------------------------------------- |
| `E*`     | zero or more                             |
| `E+`     | one or more                              |
| `E?`     | zero or one                              |
| `E{m}`   | exactly `m`                              |
| `E{m,}`  | at least `m`                             |
| `E{m,n}` | from `m` through `n`, inclusive          |

Occurrences are consecutive and each occurrence matches the same operand
language. An interval count is a sequence of decimal digits interpreted as a
nonnegative integer. A count over `RE_DUP_MAX`, a second count below the first,
more than two counts, or nonnumeric interval content does not form a valid
interval and is outside the defined ERE grammar. An implementation that rejects
such content can use `REG_BADBR`.

`{` that does not participate in one of the three valid interval forms has
undefined meaning; it is not portably a literal. `\{` matches a literal `{`.

### 8.5 Empty occurrences

XBD 9.4.6 states the null-match restriction for an ERE matching a single
character followed by `*`, `?`, or an interval: such a repetition does not take
a null match unless that is its only available match or is needed to meet an
interval's exact or minimum count.

For a finite implementation model, this specification applies the same rule to
all nullable repeated operands and duplication forms. That broader application,
including `+` and parenthesized operands, is an interpretation of the rule's
intent rather than its literal scope. It prevents repetition from manufacturing
arbitrarily many zero-length occurrences: once the minimum is met, further
empty occurrences do not improve a longest repetition.

This rule both preserves the selected language match and gives finite meaning
to repetitions of nullable parenthesized EREs. It does not turn a participating
empty group into a nonparticipating group.

### 8.6 Repetition preference and `REG_MINIMAL`

Without `REG_MINIMAL`, every duplication is longest-preferring. Appending one
extra `?` reverses just that duplication to shortest-preferring:

```text
*?  +?  ??  {m}?  {m,}?  {m,n}?
```

The modifier changes selection, not the set of strings the duplication can
match. Its shortest choice includes the null match where the duplication's
minimum permits zero.

With `REG_MINIMAL`, every duplication is shortest-preferring by default, and a
following repetition modifier reverses just that duplication back to
longest-preferring. The flag is applicable only to ERE compilation.

The modifier is part of Issue 8 ERE syntax. It is not an adjacent-duplication
extension. A third postfix duplication, or any other sequence of adjacent
duplication symbols after accounting for this one modifier, has undefined
results.

## 9. Alternation, anchors, and precedence

### 9.1 Alternation

`A|B` accepts a substring accepted by either `A` or `B`. Both branches are
nonempty. Selection is by the POSIX match rules, not by textual branch order;
the first branch does not win merely because it appears first.

An alternation of single-character expressions inside parentheses is itself an
ERE matching one character, but it remains a capturing group and follows the
same observable match rules as any other alternation.

### 9.2 Anchors

Outside brackets, `^` and `$` are zero-width anchors wherever they occur:

- `^` requires the beginning of the subject, with the additional line-boundary
  behavior of `REG_NEWLINE`;
- `$` requires the end of the subject, with the additional line-boundary
  behavior of `REG_NEWLINE`.

`REG_NOTBOL` and `REG_NOTEOL` can suppress recognition of the subject's outer
boundaries, as detailed in Section 12.4. They do not suppress the internal
newline boundaries added by `REG_NEWLINE`.

Anchors in an impossible position still form a valid ERE. For example, `a^b`
and `e$f` compile but cannot match because consuming a preceding or following
character conflicts with the asserted outer boundary.

### 9.3 Precedence

From highest to lowest, ERE operators bind as follows:

1. the inner delimiters of collating symbols, equivalence classes, and
   character classes;
2. escaped special characters;
3. the complete bracket expression;
4. parenthesized grouping;
5. duplication and its repetition modifier;
6. concatenation;
7. anchoring; and
8. alternation.

Alternation therefore splits complete branches: `abba|cde` means `(abba)|(cde)`.
Parentheses override the ordinary structure and always capture.

## 10. Locale-sensitive matching

### 10.1 Case-sensitive default

Without `REG_ICASE`, literal and bracket membership uses the encoded characters
and the current locale data exactly as described above. Graphic resemblance,
Unicode normalization, and locale collation equality do not make ordinary
characters equal. Equivalence classes are the explicit mechanism for primary
collation equivalence.

### 10.2 Case-insensitive mode

With `REG_ICASE`, matching is closed over the current `LC_CTYPE` `toupper` and
`tolower` counterpart mappings. If a subject would match case-sensitively,
replacing any of its characters by a defined case counterpart must also match.
This applies throughout the ERE, including literals, bracket expressions,
explicit collating elements, and characters within multi-character collating
elements.

For example, if `Ch` is a locale-defined multi-character collating element,
case-insensitive `[[.Ch.]]` also recognizes the counterpart combinations `ch`,
`cH`, and `CH` when those character mappings exist.

The closure applies to the result of a case-sensitive non-matching list, not to
its positive list before inversion. Thus case-insensitive `[^a]` matches both
`A` and `a` in the POSIX locale: `A` matches case-sensitively, so its lowercase
counterpart must also match case-insensitively. This deliberately follows the
literal Issue 8 rule. An implementation that first adds case counterparts to
the positive list and then inverts it instead excludes both characters and
does not implement that closure.

POSIX case-insensitive matching is locale character mapping, not unrestricted
Unicode case folding. An implementation must not add multi-character folds or
normalization unless they follow from the active POSIX locale's specified
character mappings and collating elements.

## 11. `<regex.h>` interface

### 11.1 Required declarations

Including `<regex.h>` defines the structures and constants used by the regular
expression functions and makes `size_t` available as specified by
`<sys/types.h>`. It declares these functions; they may additionally be macros:

```c
int regcomp(regex_t *restrict preg,
            const char *restrict pattern,
            int cflags);

size_t regerror(int errcode,
                const regex_t *restrict preg,
                char *restrict errbuf,
                size_t errbuf_size);

int regexec(const regex_t *restrict preg,
            const char *restrict string,
            size_t nmatch,
            regmatch_t pmatch[restrict],
            int eflags);

void regfree(regex_t *preg);
```

The implementation may make these structure types larger than the required
visible members:

```c
typedef struct {
    /* implementation fields are permitted */
    size_t re_nsub; /* number of parenthesized subexpressions */
} regex_t;

typedef struct {
    regoff_t rm_so; /* byte offset of substring start */
    regoff_t rm_eo; /* byte offset one past substring end */
} regmatch_t;
```

`regoff_t` is a signed integer type capable of representing the largest value
representable by either `ptrdiff_t` or `ssize_t`. Its positive range must
therefore reach both types' maxima; `int` is insufficient on an ABI where
either required maximum is larger than `INT_MAX`.

### 11.2 Compilation flags

These constants are valid bits for the `cflags` argument:

| Flag           | Required effect                                                                                                                                            |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `REG_EXTENDED` | Parse `pattern` as an ERE. Without it, the pattern is a BRE, which this document does not specify.                                                         |
| `REG_ICASE`    | Apply the case-insensitive rules of Section 10.2.                                                                                                          |
| `REG_MINIMAL`  | Make every ERE duplication shortest-preferring by default and invert the effect of its following repetition modifier. It applies only with `REG_EXTENDED`. |
| `REG_NOSUB`    | Compile for success/failure reporting only; `regexec()` must ignore `pmatch`.                                                                              |
| `REG_NEWLINE`  | Apply the newline exceptions in Section 12.3.                                                                                                              |

`cflags` is the bitwise-inclusive OR of zero or more supported flags. The
implementation may define additional `REG_` flags. This specification does not
assign behavior to unsupported bits or to `REG_MINIMAL` without
`REG_EXTENDED`.

### 11.3 Execution flags

These constants are valid bits for the `eflags` argument:

| Flag         | Required effect                                                               |
| ------------ | ----------------------------------------------------------------------------- |
| `REG_NOTBOL` | The start of `string` is not considered a beginning-of-line boundary for `^`. |
| `REG_NOTEOL` | The end of `string` is not considered an end-of-line boundary for `$`.        |

Internal newline boundaries created by `REG_NEWLINE` remain active regardless
of these execution flags.

### 11.4 Error constants

`<regex.h>` defines at least these nonzero symbolic error results:

| Constant       | Meaning when returned                                                                              |
| -------------- | -------------------------------------------------------------------------------------------------- |
| `REG_NOMATCH`  | `regexec()` found no match.                                                                        |
| `REG_BADPAT`   | Invalid regular expression.                                                                        |
| `REG_ECOLLATE` | Invalid collating element reference.                                                               |
| `REG_ECTYPE`   | Invalid character-class type reference.                                                            |
| `REG_EESCAPE`  | Pattern ends with a backslash.                                                                     |
| `REG_ESUBREG`  | A BRE `\digit` reference number is invalid or erroneous; ERE does not use it.                      |
| `REG_EBRACK`   | Bracket imbalance.                                                                                 |
| `REG_EPAREN`   | Parenthesis imbalance.                                                                             |
| `REG_EBRACE`   | Brace imbalance.                                                                                   |
| `REG_BADBR`    | Invalid interval content: nonnumber, excessive count, too many counts, or lower bound above upper. |
| `REG_ERANGE`   | Invalid range endpoint.                                                                            |
| `REG_ESPACE`   | Insufficient memory.                                                                               |
| `REG_BADRPT`   | `?`, `*`, or `+` lacks a valid preceding expression.                                               |

An implementation may add error constants whose names begin with `REG_`.
If multiple errors apply, any applicable code may be returned; detection order
is unspecified. When `regcomp()` reports an invalid RE, it may use the general
`REG_BADPAT` code rather than the more precise code. The existence of a precise
code does not turn every grammar violation into a construct that XBD explicitly
requires to be rejected; Section 14 preserves that distinction.

## 12. Compilation and execution

### 12.1 `regcomp()`

`regcomp(preg, pattern, cflags)` parses and compiles the NUL-terminated pattern
according to the active locale and flags. With `REG_EXTENDED`, the language is
the ERE specified here.

On success it returns zero and initializes `*preg` as a compiled regular
expression. If `REG_NOSUB` was not supplied, it sets `preg->re_nsub` to the
number of parenthesized subexpressions in `pattern`. POSIX does not require a
particular `re_nsub` value when `REG_NOSUB` was supplied.

On failure it returns a nonzero error code and leaves the contents of `*preg`
undefined. If an explicitly invalid RE is detected, the result may be
`REG_BADPAT` or an applicable more-specific compilation code. The pointer may
nevertheless be passed to `regerror()` with that returned code for diagnostic
text; it is not a live compiled expression for `regexec()` or `regfree()`.

### 12.2 `regexec()` search result

`regexec(preg, string, nmatch, pmatch, eflags)` compares the NUL-terminated
subject with a successfully compiled, not-yet-freed expression:

- it returns zero when the selected match exists;
- it returns `REG_NOMATCH` when no match exists; and
- it may return another nonzero error code if execution fails, such as
  `REG_ESPACE` when required memory is unavailable.

Issue 8 is internally inconsistent here. The function description permits a
nonzero result for either no match or an error, and `regerror()` accepts a code
from `regexec()`, but the RETURN VALUE section names only `REG_NOMATCH` as the
nonzero result. This specification preserves the execution-error outcome from
the description rather than treating `regexec()` as strictly binary.

The selected match is the unanchored or anchored match established by Sections
4 through 10 and the flags below. A successful match can have zero length,
including at the subject's terminating NUL position.

### 12.3 `REG_NEWLINE`

Without `REG_NEWLINE`, newline is ordinary in both pattern and subject. Dot and
a non-matching list can consume it, and only the outer subject boundaries
satisfy anchors.

With `REG_NEWLINE`, newline remains ordinary except for exactly these changes:

1. Dot outside a bracket expression cannot consume newline.
2. No non-matching list can consume newline. A positive matching list that
   explicitly includes newline can still consume it.
3. `^` also matches the zero-length position immediately after every newline in
   the subject, regardless of `REG_NOTBOL`.
4. `$` also matches the zero-length position immediately before every newline
   in the subject, regardless of `REG_NOTEOL`.

A literal newline in the pattern can still consume a subject newline. Therefore
`REG_NEWLINE` does not by itself guarantee that a match remains within one
line.

### 12.4 `REG_NOTBOL` and `REG_NOTEOL`

`REG_NOTBOL` suppresses `^` at the zero-length position before the first
character of `string`. `REG_NOTEOL` suppresses `$` at the zero-length position
after the last character of `string`.

These flags describe the boundaries of the particular pointer passed to
`regexec()`. They support repeated searches on successive suffixes without
falsely treating every suffix as a new line. They do not remove a boundary
immediately after or before a subject newline when the compiled expression has
`REG_NEWLINE`.

### 12.5 `pmatch` activation and size

If `nmatch` is zero, or if `REG_NOSUB` was used at compilation, `regexec()` must
ignore `pmatch` completely. It must not read it, write it, or require it to be a
valid pointer.

Otherwise, the caller supplies an array of at least `nmatch` elements and
`regexec()` fills every element on success:

- `pmatch[0]` describes the entire selected match;
- `pmatch[i]`, for `i >= 1`, describes capturing subexpression `i` when that
  index exists and is among the requested elements; and
- every requested element that is unused or corresponds to a nonparticipating
  subexpression receives `rm_so == -1` and `rm_eo == -1`.

If the ERE has more subexpressions than fit, matching still occurs and only the
first `nmatch` entries, counting the whole ERE as entry zero, are reported. If
the array is larger than `re_nsub + 1`, all remaining entries are set to `-1`.
After a nonzero `regexec()` result, this specification makes no promise about
the prior or resulting contents of an active `pmatch` array.

### 12.6 Offsets

For a participating substring:

- `rm_so` is its byte offset from the start of the exact `string` pointer passed
  to this call; and
- `rm_eo` is the byte offset of the first byte after that substring.

The interval is half-open: `[rm_so, rm_eo)`. Offsets count bytes, even though
matching and longest/shortest comparisons count characters. For a zero-length
match, both offsets are the byte offset of the following character or, at the
end, the terminating NUL.

### 12.7 Which capture occurrence is reported

A parenthesized subexpression may participate several times because it is
repeated, or may not participate because of zero repetition or an unselected
alternative. Determine each requested capture as follows:

1. For a group not contained in another group, report its last participation
   in the selected whole match.
2. If such a group did not participate, report `-1, -1`. Nonparticipation
   includes an operand selected zero times by `*`, `?`, or an interval whose
   minimum is zero, and a group in an unselected alternative.
3. For a group directly nested inside reported group `j`, apply rules 1 and 2
   within the particular substring reported for group `j`, not across earlier
   participations of `j`. Offsets remain relative to the original `string`.
4. If an enclosing reported group has `-1, -1`, every group contained in it
   also has `-1, -1`.
5. A group that participates by matching the empty string is reported with two
   equal nonnegative offsets; it is not reported as `-1, -1`.

Apply the nesting rule recursively. For example, if an outer repeated group is
reported from its final iteration and an inner optional group did not
participate in that iteration, the inner group is `-1, -1` even if it
participated in an earlier outer iteration.

## 13. Error text, lifetime, and concurrency

### 13.1 `regerror()`

`regerror()` maps a nonzero code from `regcomp()` or `regexec()` to an
implementation-chosen printable, NUL-terminated message. The caller must pass
the last nonzero code returned with that `preg`; otherwise the generated
message is unspecified.

The caller may pass a null `preg` for a code previously returned by one of
those functions. A corresponding message is still generated but may be less
detailed.

The return value is always the number of bytes required for the full message,
including its terminating NUL:

- if `errbuf_size` is zero, `errbuf` is ignored and the function only reports
  the required size;
- if the full message fits, it is stored including the NUL; and
- if it does not fit and `errbuf_size` is nonzero, the stored prefix is
  truncated as necessary and NUL-terminated.

The text of the message is unspecified.

The regex functions report their regular-expression status through return
values, not through a required `errno` error. Apart from the explicit
`regfree()` preservation rule below, this specification does not promise that
the other calls leave an existing `errno` value unchanged.

### 13.2 `regfree()` and object lifetime

`regfree(preg)` releases every resource associated with a successful
`regcomp()` on that object and returns no value. For such a valid object that
has not already been freed, `regfree()` must not change `errno`.

After `regfree()`, that object is no longer a compiled expression. Passing an
object to `regexec()` or `regfree()` that was not successfully initialized by
`regcomp()`, or that has already been freed, has undefined behavior.

### 13.3 Thread use

POSIX functions are thread-safe unless specifically exempted; these four are
not exempted. `regexec()` receives `preg` through a pointer-to-const and must
support ordinary simultaneous use of the same compiled expression without
placing per-execution match results in the shared `regex_t` object. The caller
must still avoid data races such as freeing the object or changing process
locale concurrently with its use unless another POSIX rule makes those actions
safe.

## 14. Complete boundary ledger

This section gathers every ERE boundary called out by Issue 8. It is part of the
implementation specification: tests must not accidentally demand one result in
an area where POSIX permits several.

### 14.1 Required errors

| Condition                                                                                                | Requirement                                                                                                                                                  |
| -------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `[.element.]` names a string that is not a collating element in the active locale                        | The RE is invalid and the consumer must report an error. `regcomp()` may use `REG_BADPAT` or `REG_ECOLLATE`.                                                 |
| A POSIX-locale range denotes an empty set and the implementation chooses the "invalid" permitted outcome | Compilation must report an error for that implementation choice.                                                                                             |
| A delimiter-shaped bracket expression is assigned the permitted "invalid" outcome                        | Compilation must report an error for that implementation choice.                                                                                             |
| Memory needed to compile cannot be obtained                                                              | Compilation cannot succeed; `REG_ESPACE` is the standard code denoting this condition. Capacity guarantees apply when the necessary resources are available. |

The standard error vocabulary also provides precise codes for malformed
brackets, groups, braces, classes, intervals, and repetitions. For syntax not
explicitly designated invalid by XBD Chapter 9, rejecting it is one conforming
interpretation of an undefined spelling, rather than the only interpretation.

### 14.2 Undefined pattern spellings

The following produce undefined results. A conforming implementation can reject
them, assign an extension meaning, or in some cases treat characters literally:

| Condition                                                                            | Examples or notes                                                                                       |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| Empty entire ERE, empty group, or empty branch                                       | empty pattern, `()`, `\|a`, `a\|`, `a\|\|b`                                                           |
| Unescaped `[` that does not form a complete bracket expression                       | bracket imbalance is commonly reported as `REG_EBRACK`                                                  |
| Unmatched opening parenthesis or otherwise malformed grouping                        | parenthesis imbalance is commonly reported as `REG_EPAREN`                                              |
| Trailing backslash                                                                   | commonly reported as `REG_EESCAPE`                                                                      |
| Backslash before an ordinary ERE character                                           | `\a`, `\1`, and `\n` have no POSIX ERE meaning                                                          |
| A bracket special opener lacks the required valid content and matching closer        | malformed `[.`, `[=`, or `[:` forms can be rejected with a bracket-, collation-, or class-related error |
| A character class name is not defined by the current locale                          | it is not the grammar's `class_name` token and can be rejected with `REG_ECTYPE`                        |
| `*`, `+`, `?`, or `{` first in an ERE or immediately after `\|`, `^`, `$`, or `(`    | the postfix operator lacks a permitted operand                                                          |
| `{` not part of a valid interval                                                     | includes malformed contents and values outside the required interval domain                             |
| `\|` first or last, after `\|` or `(`, or before `)`                                 | these are the empty-branch cases plus the explicitly listed placements                                 |
| More than one adjacent duplication after recognizing an optional repetition modifier | examples include `a**`, `a+*`, `a{2}+`, and a third `?` after `a??`                                     |
| One range's ending point is also the next range's starting point                     | `[a-m-o]`                                                                                               |

This table does not include explicitly invalid collating symbols, which must be
errors, or the unspecified choices in the next section.

### 14.3 Unspecified matching or parsing choices

| Condition                                                                                              | Permitted choice                                                                                              |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| A matching list could implicitly recognize a multi-character collating element                         | It may or may not do so.                                                                                      |
| A non-matching list could recognize a multi-character collating element absent from its positive list  | It may or may not do so.                                                                                      |
| The multi-character part of a character class expression                                               | Any implementation-chosen set is permitted.                                                                   |
| A range outside the POSIX locale                                                                       | Validity and matched set are wholly unspecified.                                                              |
| An equivalence class is used as a range endpoint                                                       | Result is unspecified.                                                                                        |
| A POSIX-locale range's represented set is empty                                                        | It may match nothing or make the bracket expression invalid.                                                  |
| A list of at least three elements begins and ends with the same one-character `.`, `=`, or `:` element | It may be parsed as the corresponding special bracket construct, as an ordinary matching list, or as invalid. |
| `regerror()` receives a code that was not the last nonzero result for that `preg`                      | Generated message is unspecified.                                                                             |
| `regcomp()` succeeds with `REG_NOSUB`                                                                  | The value of `re_nsub` is not specified.                                                                      |

### 14.4 Defined spellings often mistaken for errors

- An unmatched `)` is ordinary because `)` becomes special only when paired
  with a preceding `(`.
- Unescaped `]` and `}` outside a bracket expression are ordinary. Escaped
  `\]` and `\}` are also explicitly defined and match the literal characters.
- `^` and `$` are anchors everywhere outside brackets. They do not become
  literal merely because their placement makes a match impossible.
- Backslash is ordinary inside a bracket expression. A portable pattern does
  not use it to quote `-`, `]`, `^`, or another bracket character.
- A positive bracket list can match newline under `REG_NEWLINE`; only dot and
  non-matching lists receive the newline exclusion.
- `*?`, `+?`, `??`, and an interval followed by `?` are standard Issue 8
  repetition forms.
- A valid one-character collating symbol such as `[[.a.]]` is required even in
  the POSIX locale.
- An equivalence class containing an element with no peer still matches that
  element as a collating symbol.

### 14.5 API precondition failures

The general POSIX interface rules make behavior undefined for invalid pointer
arguments and values outside a function's domain unless a function description
provides another rule. Important exceptions here are intentional ignore cases:

- `pmatch` need not be valid when `nmatch == 0` or `REG_NOSUB` was used;
- `errbuf` need not be valid when `errbuf_size == 0`; and
- `preg` may be null in the specifically permitted `regerror()` case.

In contrast, `regexec()` and `regfree()` require a live successfully compiled
object. The caller must provide a `pmatch` array of at least `nmatch` entries
whenever it is not ignored.

## 15. Reference implementation model

This section is non-normative as an algorithm, but each observable checkpoint
is normative. It gives a sufficient architecture for a complete implementation.

### 15.1 Locale adapter

Expose immutable compilation-time operations for:

1. decoding the locale's characters from the NUL-terminated byte pattern;
2. querying all locale character classes and `toupper`/`tolower` mappings;
3. mapping character sequences to valid collating elements;
4. querying the primary equivalence class of an element; and
5. obtaining the POSIX-locale collation order for range construction.

For non-POSIX locales, decide and consistently implement the unspecified range
and implicit multi-character-list policies. These choices do not need to mimic
`strcoll()` by building ranges from full sorting weights; POSIX deliberately
does not require that outside its own locale.

### 15.2 Lexer and parser

1. Count pattern capacity in bytes, but decode tokens as characters.
2. Use the outside-bracket special set from Section 5.
3. Enter a distinct bracket lexer at `[`. Handle initial `^`, initial `]`, the
   three two-character inner openers, contextual `-`, and ordinary backslash.
4. Recognize interval digits as `count` only inside a syntactically possible
   interval and only within `RE_DUP_MAX`.
5. Construct alternation, branch concatenation, group, anchor, atom, and
   duplication nodes according to Section 6.
6. Number every opening group from left to right and record `re_nsub` when
   required.
7. Mark each duplication longest- or shortest-preferring from `REG_MINIMAL`
   XOR the presence of its repetition modifier.

The parser should distinguish a required invalidity from an undefined spelling
if the implementation's conformance mode needs to reject extensions selectively.

### 15.3 Match relation

Evaluate on character boundaries while retaining the corresponding byte offset
for every boundary. A transition can consume one character or the complete
character sequence of one matched collating element. Never start or end a match
inside a multibyte character.

Apply dot, lists, locale classes, collation elements, and equivalence classes
to establish case-sensitive atom transitions. With `REG_ICASE`, close accepted
subject sequences over the locale's per-character counterpart mappings. For a
non-matching list, complement the positive membership before applying that
closure. Add zero-width transitions for anchors using the compile and execution
flags. Build concatenation, alternation, and bounded or unbounded repetition
over that relation.

Prevent an unbounded nullable repetition from creating infinitely many
equivalent derivations. Section 8.5 gives the observable empty-occurrence rule.

### 15.4 Candidate selection and tags

For each subject character boundary from beginning to end, determine whether a
complete match begins there. Stop considering later starts after finding any
match at the earliest successful boundary.

Among derivations at that boundary, retain enough ordering information to
enforce:

- whole-match length and all repetition preferences;
- left-to-right subpattern preference after enclosing constraints; and
- the last-participation and recursive containment rules for captures.

An ordinary Perl-style ordered NFA is insufficient because branch discovery
order is not POSIX priority. A DFA without tags is also insufficient when
submatch offsets are requested. Tagged automata, a correctly ordered dynamic
program, or exhaustive candidate comparison can all implement the contract.

Finally convert selected character boundaries to byte offsets and fill all
requested `pmatch` entries, including `-1` entries. Skip the output path
entirely when `pmatch` is required to be ignored.

## 16. Conformance examples

These examples exercise requirements rather than extensions. Offsets are byte
offsets; the examples use single-byte POSIX-locale characters unless noted.

### 16.1 Selection and grouping

| ERE       | Subject   | Required result                                                                           |
| --------- | --------- | ----------------------------------------------------------------------------------------- |
| `b\|ab`   | `ab`      | whole match `ab` at `[0,2)`, because an earlier start wins                                |
| `a\|ab`   | `ab`      | whole match `ab`, because branch order does not beat longest match                        |
| `(a\|aa)(a*)` | `aaa` | whole `aaa`; group 1 is `aa`; group 2 is `a`                                              |
| `(ab)*`   | `abab`    | whole `[0,4)`; group 1 reports its last participation `[2,4)`                             |
| `(a)?b`   | `b`       | whole `[0,1)`; group 1 is `[-1,-1)` in descriptive notation, meaning both fields are `-1` |
| `(a*)b`   | `b`       | group 1 participates with the empty match `[0,0)`                                         |
| `.*c`     | `abc abc` | match `abc abc` through the last `c`                                                      |
| `.*?c`    | `abc abc` | match `abc` through the first `c`                                                         |
| `(.*?).*` | `abcdef`  | whole `abcdef`; group 1 is empty at `[0,0)`                                               |

The notation `[-1,-1)` in the table is only mnemonic; the actual nonparticipation
record is `rm_so == -1` and `rm_eo == -1`, not a mathematical interval.

With `REG_MINIMAL`, `.*` on `abc` selects the empty match at `[0,0)`, while
`.*?` selects `abc`: the flag changes the default and the modifier reverses it.

### 16.2 Anchors and newline

| Configuration                                         | ERE and subject                                                               | Required result                |
| ----------------------------------------------------- | ----------------------------------------------------------------------------- | ------------------------------ |
| no flags                                              | `a^` on `a`                                                                   | valid ERE, no match            |
| no flags                                              | `$a` on `a`                                                                   | valid ERE, no match            |
| no `REG_NEWLINE`                                      | `.` on newline                                                                | matches newline                |
| `REG_NEWLINE`                                         | `.` on newline                                                                | no match                       |
| `REG_NEWLINE`                                         | `[\n]` shown abstractly as a bracket containing a literal newline, on newline | matches; it is a positive list |
| `REG_NEWLINE`                                         | `[^a]` on newline                                                             | no match                       |
| compile with `REG_NEWLINE`; execute with `REG_NOTBOL` | `^b` on the `b` in subject `a\nb`                                             | matches after the newline      |
| compile with `REG_NEWLINE`; execute with `REG_NOTEOL` | `a$` on subject `a\nb`                                                        | matches before the newline     |

The `\n` appearances in the subject column are display notation for an actual
newline; POSIX ERE does not define a backslash-`n` escape.

### 16.3 Brackets and locale

In the POSIX locale:

- `[[.a.]]` matches `a`;
- `[[=a=]]` matches `a` even though no other portable character need share its
  primary weight;
- with `REG_ICASE`, `[^a]` matches both `A` and `a` under Issue 8's closure
  rule;
- `[[:digit:]]` matches exactly a portable decimal digit;
- `[^[:digit:]]` matches `A` but not `7`;
- `[a-c]` matches `a`, `b`, or `c`;
- `[]-]` matches `]` or `-`; and
- a bracket list containing backslash treats it as a literal member.

### 16.4 API reporting

- Compiling `(a)(b)` without `REG_NOSUB` sets `re_nsub` to 2.
- A successful call with `nmatch == 4` fills entries 0, 1, and 2 and sets both
  fields of entry 3 to `-1`.
- A successful match of a multibyte character reports the number of bytes in
  `rm_eo - rm_so`, while match preference counts it as one character.
- With `REG_NOSUB`, a call with nonzero `nmatch` and a null `pmatch` pointer
  must still be safe because the pointer is ignored.
- Calling `regerror(code, preg, NULL, 0)` returns the full required message size
  including its terminating NUL.

## 17. Implementation conformance checklist

A claimed complete ERE implementation should have affirmative evidence for
every item below.

### Syntax and parsing

- [ ] The defined grammar accepts every ordinary, quoted, bracket, grouping,
      concatenation, alternation, anchor, interval, and Issue 8 repetition form.
- [ ] Empty patterns, branches, and groups are kept outside the portable grammar.
- [ ] `)` is context-sensitive; unmatched `)`, `]`, and `}` are ordinary outside
      brackets.
- [ ] Escaped ordinary characters are not silently claimed as POSIX syntax.
- [ ] Group numbering is by opening-parenthesis order with no nine-group limit.
- [ ] Counts through `RE_DUP_MAX` work and the advertised value is at least 255.

### Brackets and locales

- [ ] Initial `^`, initial `]`, contextual `-`, and ordinary bracket backslash
      follow Section 7.
- [ ] One- and multi-character collating symbols are implemented and nonexistent
      elements are rejected.
- [ ] Primary equivalence classes work, including the singleton fallback.
- [ ] All twelve standard character classes and all locale-defined classes work.
- [ ] POSIX-locale ranges use its required collation sequence.
- [ ] Every non-POSIX-locale unspecified policy is kept within the permitted set.
- [ ] `REG_ICASE` uses locale counterpart mappings for every relevant
      construct, including closure after inversion for non-matching lists.

### Matching and reporting

- [ ] Earliest start, longest whole match, subpattern priority, and per-repetition
      shortest/longest preference pass adversarial ambiguous cases.
- [ ] Match length is measured in characters, including for multi-character
      collating elements.
- [ ] Nullable repetition terminates and follows the empty-occurrence rule.
- [ ] Anchors are recognized everywhere and all four enumerated newline effects
      work.
- [ ] `REG_NOTBOL` and `REG_NOTEOL` suppress only their specified outer boundary.
- [ ] Captures report last participation recursively and distinguish empty from
      nonparticipating.
- [ ] Reported offsets are byte offsets on character boundaries.
- [ ] Every active `pmatch` element is initialized, including unused elements.
- [ ] `pmatch` is untouched when `nmatch == 0` or `REG_NOSUB` applies.

### ABI and lifecycle

- [ ] `<regex.h>` supplies the required types, members, prototypes, flags, and
      error constants.
- [ ] `regoff_t` can hold the positive maximum of both `ptrdiff_t` and
      `ssize_t`.
- [ ] `regcomp()` success/failure state and `re_nsub` behavior are correct.
- [ ] `regerror()` sizing, truncation, NUL termination, and null-pointer ignore
      cases are correct.
- [ ] `regfree()` releases resources and preserves `errno` for a live object.
- [ ] The same live compiled expression supports concurrent `regexec()` calls.
- [ ] A locale change between compilation and execution is documented and
      treated as outside the defined result domain.

## 18. Source map

| Topic in this document                                          | Authoritative Issue 8 source      |
| --------------------------------------------------------------- | --------------------------------- |
| Definitions, match selection, general requirements              | [XBD 9.1 and 9.2][xbd-9]          |
| ERE atoms, syntax, repetition, alternation, precedence, anchors | [XBD 9.4][xbd-9]                  |
| Common bracket-expression rules                                 | [XBD 9.3.5][xbd-9]                |
| Normative lexical and yacc-style grammar                        | [XBD 9.5][xbd-9]                  |
| Case-insensitive closure                                        | [XBD 4.1][xbd-4]                  |
| Character classes and case mappings                             | [XBD 7.3.1][xbd-7]                |
| Collating elements, weights, and equivalence                    | [XBD 7.3.2][xbd-7]                |
| `RE_DUP_MAX` and `CHARCLASS_NAME_MAX`                           | [XBD `<limits.h>`][limits-h]      |
| Types, declarations, constants                                  | [XBD `<regex.h>`][regex-h]        |
| Compilation, execution, captures, flags, errors, and lifecycle  | [XSH `regcomp()` family][regcomp] |
| Explanatory match-selection and internationalization rationale  | [XBD Rationale A.9][xrat-a9]      |

[xbd-4]: https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap04.html
[xbd-7]: https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap07.html
[xbd-9]: https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap09.html
[limits-h]: https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/limits.h.html
[regex-h]: https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/regex.h.html
[regcomp]: https://pubs.opengroup.org/onlinepubs/9799919799/functions/regcomp.html
[xrat-a9]: https://pubs.opengroup.org/onlinepubs/9799919799/xrat/V4_xbd_chap01.html#tag_21_09
