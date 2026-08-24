# Review of the POSIX.1-2024 ERE specification document

Reviewed: `docs/POSIX-1-2024-ERE-SPECIFICATION.md`.
Date: 2026-08-24.
Method: the document's normative claims were compared against the published
Issue 8 text at `pubs.opengroup.org/onlinepubs/9799919799/` (XBD 4.1, 7, 9,
`<limits.h>`, `<regex.h>`, XSH `regcomp()` and `sysconf()`, XRAT A.4.1 and
A.9), retrieved 2026-08-24. One behavioral point was also tested on macOS;
the probe is recorded at the end of this file. The comparison covered every
claim I could trace to a standard clause. It was still a single pass, so it
is not proof of completeness.

Overall verdict: the document is faithful to Issue 8 to an unusual degree.
Almost every normative claim traces back to exact standard text. Two findings
need correction before the document can claim to be a correct and unambiguous
specification. The rest are precision nits.

## F1. Case-insensitive non-matching lists contradict XBD 4.1 (major)

Section 10.2 says: "For a non-matching list, compute case-insensitive
membership before applying the logical inverse. Thus case-insensitive `[^a]`
excludes both `a` and `A`."

The Issue 8 text requires the opposite result. XBD 4.1 states: if a string
matches case-sensitively, the same string with any character replaced by its
case counterpart shall also match case-insensitively. The string `A` matches
`[^a]` case-sensitively. The string `a` is `A` with one counterpart
replacement. Therefore Issue 8 requires case-insensitive `[^a]` to match `a`.

Rationale A.4.1 confirms this reading. Its "simple algorithm" checks each
subject character in both cases and matches if either case matches. Checking
`A` against `[^a]` succeeds, so `a` matches.

The document's rule matches tested practice, not the standard. macOS
`regcomp()` with `REG_ICASE` rejects `a` and `A` against `[^a]`, and
`printf a | grep -i '[^a]'` finds no match (see the probe record below).
Other implementations reportedly behave the same way, but I did not test
them; verify `ref/tre` and glibc before citing them as evidence.

The section is also internally inconsistent. Its first paragraph states the
4.1 closure correctly. Its non-matching-list paragraph then negates that
closure for the same construct.

Suggested fix: state the literal Issue 8 requirement, then record the conflict
with existing practice as an explicit, deliberate divergence note. Do not
present the inverted-membership rule as a POSIX requirement.

## F2. Broken Markdown tables in sections 14.2 and 16.1 (major)

Unescaped `|` characters inside table cells split the cells. GFM splits on
`|` even inside backticks. The damage hits:

- The 14.2 rows for empty branches (`\|a`, `a\|`, `a\|\|b`) and for the
  undefined `\|` placements. These rows now render as garbage columns.
- The 16.1 rows for `b\|ab`, `a\|ab`, and `(a\|aa)(a*)`. Their subjects and
  results land in the wrong columns.

The "complete boundary ledger" is unreadable exactly where it defines the
undefined alternation spellings. Escape every in-cell `|` as `\|`, or move
those examples out of tables. GFM applies the `\|` escape even inside code
spans, so `` `a\|b` `` renders as code `a|b`. A cell that must display a
literal backslash before a pipe therefore needs `\\|` in the source. After
the repair, the example semantics are correct; I verified each row against
the selection rules.

## F3. `regexec()` outcomes hide a tension in the standard (low)

Section 12.2 says `regexec()` returns zero or `REG_NOMATCH`. That follows
the normative RETURN VALUE paragraph, so it is defensible as written. But
the XSH DESCRIPTION says a nonzero return indicates "either no match or an
error", and real implementations return `REG_ESPACE` from `regexec()`.
Section 13.1 of the document already assumes `regexec()` can produce codes
for `regerror()`. Suggested fix: keep the binary rule as the standard's
letter, and add a note recording the DESCRIPTION wording and the common
`REG_ESPACE` practice. Do not present extra error returns as required.

## F4. Equivalence classes do not accept bracket metadata (minor)

Section 7.1 prose says the `collating_element` inside "an inner special
construct" can be a character that is otherwise bracket metadata. The grammar
supports that for collating symbols only: `collating_symbol` accepts
`META_CHAR`, but `equivalence_class` accepts only `COLL_ELEM_SINGLE` and
`COLL_ELEM_MULTI`. So `[.-.]` is inside the grammar while `[=-=]` is not.
Scope the metadata allowance to `[. .]`.

## F5. The empty-occurrence rule is broader than the standard's letter (minor)

Section 8.5 applies the null-repetition restriction to every repetition.
XBD 9.4.6 literally covers "an ERE matching a single character repeated by
an `*`, `?`, or an interval expression". Per XBD 9.1, that phrase still
includes a parenthesized ERE such as `(a)` that matches one character. The
actual gaps are `+`, and parenthesized operands that match multiple
characters or the null string. The BRE version covers subexpressions; the
ERE version does not. The document's generalization is the right engineering
choice, but it should be labeled as an interpretation of intent, not as
standard text.

## F6. POSIX-locale collation of non-portable characters (minor)

Section 7.7 says additional characters follow the portable set with unique
primary weights. XBD 7.3.2.6 adds a determinism rule the document omits:
when the listed characters have ASCII encoding, the remaining characters
collate in ascending coded order. Otherwise their order is unspecified. This
affects ranges with non-portable endpoints in ASCII-based POSIX locales.

## F7. Wording of match-selection step 2 (nit)

"Choose the longest possible whole match, subject to the required shortest
preference of any affected repetition" reads as a contradiction on first
pass. The following steps and the examples do resolve it, so this is a
wording suggestion only. The sentence could state the priority directly, as
the rationale does: the affected repetition takes the shortest possible
matching prefix instead of the longest.

## F8. Cross-reference the delimiter ambiguity (nit)

Section 7.2 says the `[.`, `[=`, and `[:` openers "must be processed as the
start of" their constructs. Section 7.9 carves out the delimiter-shaped
outer lists where that processing choice is unspecified. The two sections
are compatible, but a reader of 7.2 alone can miss the carve-out. Add a
pointer from 7.2 to 7.9.

## Claims that look wrong but are correct

These were verified against the published text and need no change:

- Unmatched `)` is an ordinary character. XBD 9.4.3 and the 9.5.1 SPEC_CHAR
  note both say so; the grammar-precedence rule does not override it.
- `_POSIX_RE_DUP_MAX` is the Issue 8 name, value 255. `_POSIX2_RE_DUP_MAX`
  survives only as an alias. Both `{RE_DUP_MAX}` and `{CHARCLASS_NAME_MAX}`
  are runtime-increasable, but only `{RE_DUP_MAX}` has a `sysconf()` query
  (`_SC_RE_DUP_MAX`); the standard defines no variable for
  `{CHARCLASS_NAME_MAX}`. The specification's section 3.4 claims `sysconf()`
  only for `{RE_DUP_MAX}`, which is correct.
- `regfree()` must not modify `errno` for a live object (Defect 385).
- `REG_MINIMAL` and the `?` repetition modifier semantics, including the
  `.*?c` whole-match example, match XBD 9.4.6 and XSH exactly.
- The five capture-reporting rules in section 12.7, including the recursive
  nesting rule, are verbatim-equivalent to XSH `regcomp()`.
- Unused `pmatch` elements up to `pmatch[nmatch-1]` are filled with -1.
- The section 7.9 delimiter-shaped ambiguity rule is real (9.3.5, last item).
- `a^b` and `e$f` as valid-but-unmatchable EREs are the standard's own
  examples in 9.4.9.
- The `[[.Ch.]]` case-insensitive example is verbatim from XBD 4.1.
- POSIX-locale additions are allowed only for `cntrl` and `punct` (7.3.1.1).
- `digit` and `xdigit` have exact membership in all locales; `alnum` is
  exactly `alpha` plus `digit`.
- The POSIX locale and the C locale are identical (XBD 7.2).
- Empty-set ranges are unspecified (match nothing or invalid); `[a-m-o]` is
  undefined; equivalence-class range endpoints are unspecified.
- The quoted-character list, special-character list, and precedence table
  match 9.4 and 9.5. The document's EBNF is a faithful synthesis rather than
  an exact copy: it folds the 9.4.6 repetition modifier into the grammar,
  which the 9.5.3 yacc grammar and the precedence table do not show.
- IGNORE-weight elements form one equivalence class (7.3.2.4).

## Evidence record for the F1 probe

Platform: macOS (Darwin 27.0.0), system libc `regcomp()`, system `grep`,
default locale environment. Test program:

    #include <regex.h>
    #include <stdio.h>
    int main(void) {
        regex_t re;
        regcomp(&re, "[^a]", REG_EXTENDED | REG_ICASE);
        printf("%d %d %d\n",
               regexec(&re, "a", 0, NULL, 0),
               regexec(&re, "A", 0, NULL, 0),
               regexec(&re, "b", 0, NULL, 0));
        return 0;
    }

Observed output: `1 1 0` (REG_NOMATCH for `a` and `A`, match for `b`).
Shell probe: `printf a | grep -i '[^a]'` exited nonzero with no output.
