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

The user set a goal: build a cleanroom Go implementation of the ERE
specification in `go0/`, memory efficient, UTF-8 only, with the CLDR
locale tables. I created the `revera` Go module and its `locale` package.
A generator converts `src/rv_locale_data.inc` into a compact little-endian
blob embedded with `go:embed`. All lookups read the blob in place and
allocate nothing. A differential dump over 19 locale selections produced
output bit-identical to the C implementation. Progress notes live in
`go0/NOTES.md`.

Continued the Go implementation. Wrote the ERE parser with the POSIX
error codes, the bracket-expression compiler, and a reference matcher
that enumerates every parse and applies the section 4.3 selection order
directly. Then built the real engine: phase A is a single-pass parallel
NFA that finds the selected start and end with minimal-repetition
counters and a pooled, allocation-free workspace; phase B resolves
captures with a memoized best-parse search over the selected span only.
Conformance tests cover every spec section 16 example. Differential
tests compare the engine against the reference on thousands of random
patterns across ICase, Newline, and Minimal, against Go regexp's POSIX
mode for whole-match selection on long subjects, and against the cs
locale's ch collating element. The race detector is clean and the module
cross-compiles for 386, arm64, and s390x. Benchmarks show zero
allocations per match on the capture-free path.

A swival review of the module found two real bugs. The capture solver
could panic on equivalence-class spans longer than eight scalars, and
interval expansion rejected 20-byte patterns against the 256-byte
capacity rule. Both are fixed: the span bound now lives in the matcher,
and oversized expansions compile without a program, answer through a
minimum-match-length fallback, and report ESpace only when the subject
could actually need the huge program. The 64-slot counter mask became an
overflow list. I then added a scan fast path derived from the start
closure: when no thread is live, the executor skips to the next possible
first byte. Literal search went from 82 MB/s to about 10 GB/s and
no-match scans to about 50 GB/s. A native fuzz target compared engine
and oracle over 12 million cases without a mismatch. A full-Unicode
round-trip test validates the inverse case tables.

A 20,000-case differential against macOS libc, with full capture
vectors, anchors, and negated classes, exposed a misreading of the
empty-occurrence rule: a null repetition must take one null occurrence
when its operand can match null. Both engines now implement the fixed
reading; agreement with libc is 19,982 of 20,000, and every remaining
difference falls into two documented libc quirks (interval
nonparticipation and skipped leftmost empty matches) where this
implementation follows the specification text. The capture solver is
now pooled, staticcheck is clean, and dead code is removed.

The second swival round reported two more findings, now fixed with
regression tests: the minimum-length fallback undercounted brackets that
hold only multi-character collating symbols, and overflow counter slots
wrapped at 65,536 because of a uint16 narrowing.

The third swival round found that capture requests hit the work limit on
ordinary patterns: ((a|aa)*) with captures failed at 600 characters. The
repetition memo key now folds counts past the minimum for unbounded
maxima, and split loops are clamped by precomputed per-node length
bounds; the same case now runs 5,000 characters in 36 milliseconds. The
oversized-expansion fallback was reworked from whole-program to subtree
granularity: a huge repetition compiles to a dead-end instruction and
the program stays exact for subjects shorter than that subtree's
minimum match length, so (x|huge) matches "x" instead of reporting
ESpace. Added Kelvin-sign case-closure tests and re-ran the full
validation set: differentials, fuzzing, race detector, staticcheck, and
the 20,000-case libc comparison all pass.

The fourth swival round audited the new clamping, folding, and pruning
mechanisms. It confirmed them sound and found one bug: satAdd could
overflow on 32-bit platforms before its saturation clamp. Fixed with
int64 arithmetic and re-verified with 386 and s390x cross-builds. The
remaining conservative ESpace behaviors of the pruned-program gate are
documented in go0/NOTES.md.
