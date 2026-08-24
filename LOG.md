# Work log

## 2026-08-24

The user asked for a correctness and ambiguity review of
`docs/POSIX-1-2024-ERE-SPECIFICATION.md` against POSIX.1-2024 Issue 8.

I downloaded the published Issue 8 pages (XBD 4.1, 7, 9, `<limits.h>`,
`<regex.h>`, XSH `regcomp()`, XRAT) and compared every checkable claim.
I also tested `REG_ICASE` `[^a]` behavior on macOS libc and `grep -i`.
I wrote the findings to `docs/POSIX-1-2024-ERE-SPECIFICATION-REVIEW.md`:
two required fixes (the `REG_ICASE` non-matching-list rule contradicts
XBD 4.1; unescaped `|` breaks the tables in 14.2 and 16.1), plus several
precision items. I did not change the specification itself.

The user then applied all eight findings to the specification. I verified
the updated document: the `REG_ICASE` sections now state the literal
XBD 4.1 closure consistently (7.3, 10.2, 15.3, 16.3, 17), the grammar
distinguishes collating-symbol and equivalence-class content, and the
`regexec()` tension is documented. I re-rendered the file with `cmark-gfm`;
all eleven tables have consistent column counts. One follow-up remains:
record the closure-versus-practice divergence in
`docs/TRE-POSIX-ERE-DIVERGENCES.md` once TRE's `[^a]` behavior is probed.

The user then asked me to run that probe. I built a disposable copy of
`ref/tre` (commit `71bfcaf`, default features, static library) and tested
`REG_ICASE` non-matching lists in the POSIX locale. TRE excludes both
cases: `[^a]`, `[^[:lower:]]`, and `[^a-z]` all refuse subjects the
XBD 4.1 closure requires them to match, while positive-direction closure
works. The source confirms the mechanism: counterpart items are added to
the positive list before negation (`lib/tre-parse.c:380-414`, `441-457`),
and negated classes check both cases (`lib/tre-match-utils.h:204-215`).
I recorded this as gap 6 in `docs/TRE-POSIX-ERE-DIVERGENCES.md` with the
probe transcript, verified all tables render, and removed the build tree.

A `swival` pass over the review file caught two errors in my own text.
I had claimed a `sysconf()` query for `{CHARCLASS_NAME_MAX}`; none exists,
only `_SC_RE_DUP_MAX` does. I had also overstated the `regexec()` finding;
the specification's binary return rule follows the normative RETURN VALUE
text. I corrected both, narrowed the null-repetition finding, and added
the `REG_ICASE` probe as a reproducible evidence record.

The locale-table implementation now projects CLDR 48.2 and Unicode 17 into
hard-coded `LC_CTYPE` and ERE-specific `LC_COLLATE` tables. It covers all
1,122 CLDR main locales and 5,782 locale/collation selectors, plus the `C` and
`POSIX` locale. The allocation-free C runtime, public behavior tests, complete
selector audit, table invariants, deterministic regeneration check, and
sanitizer runs pass.
