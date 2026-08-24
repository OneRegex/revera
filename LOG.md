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

The user then ran /simplify on the go0 import commit. Four review
agents (reuse, simplification, efficiency, altitude) produced about
thirty distinct findings. I applied most of them. The verbatim
duplicates between the capture solver and the oracle (addCountersInt,
bracketSpanOK, assignCaptures) are gone; the solver and the oracle now
share addCounters, assignCaps, and a bracketSet.matchesSpan method.
The hand-rolled UTF-8 decoder now wraps utf8.DecodeRuneInString. The
solver's three seen/memo map pairs collapsed into single maps with
comma-ok lookups, and the []*ptree{nil} sentinel became a small
repResult struct. Bracket matching context (locale, ICase, Newline) is
now bound into bracketSet at compile time, so the three per-call
derivation sites disappeared. The locale package gained ClassMask (one
table lookup instead of up to twelve), an internal collatingElementID
that halves PrimaryEqual work, a cached max sequence length, and a
table-driven ClassByName. The executor probes only the multi-character
lengths a bracket can actually take, skips the counter copy on
maskless instructions, and an init check ties maxElemAhead to the
locale data. Smaller cleanups: clear(), min(), slices.SortFunc,
BinarySearchFunc, []rune conversion, strings.IndexByte,
utf8.AppendRune, slices.Clip, a shared trivialNullMatch helper, a
bolAt helper, and dead weight removal (slotTable.count, runPhaseA's
always-nil error, walk's unused post-order callback). The genlocale
generator lost its insertion sort and its duplicate count tables; the
regenerated blob is byte-identical. Skipped with reasons: folding
minMatchChars into minL (the caps differ on purpose), sort.Search for
the locale binary searches (hot paths), self-identifying blob
sections (format change), byte-slice blob (string is deliberate), and
counter caching on parse trees (needs benchmark-driven design). All
tests, the race detector, and the differential suite pass; every
benchmark is equal or better, captures are 30 percent faster with one
less allocation.

The user then asked me to fix the two pre-existing issues that swival
had flagged. First, the failMin gate returned ESpace without trying
the surviving branches. Exec now runs the pruned program for
existence-only calls past the failMin bound; a match it finds is
genuine because pruning only removes possibilities. A miss and any
offset request still report ESpace. Second, an equivalence class
counted a minimum of one character even when no single character
belongs to it. The locale package gained MinEquivLength, which scans
the equivalence pair sections for the shortest primary-equal element,
and bracketMinChars uses it. An exhaustive scan over all scalars
confirmed the pair-section result for Czech ch (two characters);
Danish aa reports one through å. Added regression tests that assert
the pruned failMin values, so they cannot pass vacuously, plus locale
tests for MinEquivLength. Updated NOTES.md. Full tests, the race
detector, and a swival review pass.

The user then asked me to address the open items from go0/NOTES.md.
Two boxes were open: one-pass detection and the manual libc harness.
For one-pass, Compile now proves parse uniqueness instead of analyzing
selection orderings, which satisfies the study document's proof
obligation directly. A node qualifies through fixed-length repetition
operands, at most one variable-length concatenation child, and
alternations selected by span length or one-character lookahead over
exact disjoint first sets. When the proof succeeds, phase B fills the
groups from one verified deterministic walk in onepass.go; any
inconsistency falls back to the solver, so a walk defect can only cost
speed. On a 4,000-character span the walk runs in 30 microseconds with
zero allocations where the solver needs 17 milliseconds and 68 MB.
The libc harness became a real test: internal/libcre wraps regcomp and
regexec through cgo, and libc_differential_test.go (macOS only)
replays the seeded 20,000-case corpus, classifying the two documented
divergence classes and failing on anything else; it reproduces exactly
the 18 known cases. I also fixed a sentinel dual-use the previous
swival review pointed out: failMin now uses failMinNone instead of
lenInf, which a saturated pruned minimum can legitimately reach, and
the one-pass length rules refuse saturated bounds for the same reason.
New tests cover detection, the section 12.7 clearing in the walk, and
a three-way random comparison of walk, solver, and oracle. Full tests,
the race detector, a 30-second fuzz run with 5.5 million executions,
no-cgo and 386 cross builds all pass.

The closing swival review found no soundness hole in the one-pass
path and endorsed the failMinNone sentinel. It flagged one blind spot
in the new libc test: a regression that copies a libc bug would only
shrink the known-divergence count, and the test would still pass. The
test now asserts the exact count of 18 for the seeded corpus, and a
table test pins one exemplar per divergence class with the expected
result on both sides, so convergence toward libc fails loudly.

The user then asked for resource contracts: a Compile variant that also
returns, for a given maximum input length, the upper bound of stack
usage, heap usage, and algorithmic complexity per backend, so an
application can refuse an expensive expression before running it.
CompileWithContract in go0/contract.go now returns a Contract with one
BackendContract (heap, stack, steps) for the phase A matcher, one for
the one-pass capture walk when the proof holds, and one for the capture
solver, which stays the guaranteed ceiling because the walk falls back
to it. Heap counts the explicit allocations with fixed 64-bit sizes, as
requested; allocator and collector overhead stays out. Stack multiplies
the deepest call chain by a fixed frame estimate, and steps count
abstract operations, clamped by the solver work limit. To make the
matcher bound linear, the phase A closure queue now compacts duplicate
entries once it passes twice the program length. A first attempt
checked queue membership inside relax; that broke its inlining and cost
12 percent on the ambiguous benchmark, so the check moved to the cold
compaction pass and the benchmarks returned to noise. Two swival review
rounds ran. The first found a flaky pool assertion and a test that
bypassed the Exec path; phase A gained runPhaseAWith so a test can own
its workspace. The second found real undercounts: the solver arenas
hand out whole 256-element chunks, the one-pass walk clears nested
groups per visit, the equivalence-class test recurses per element
character, the decoded window struct escapes, and the oversized-program
fallback can allocate an error. Every formula now covers these, the
clamped MaxInput is reported back, and a unit test pins the queue
compaction semantics. Full tests, the race detector, 386 and no-cgo
builds, and a 30-second fuzz run with 6.5 million executions all pass.

The user then ran the simplify pass: four parallel review agents
covering reuse, simplification, efficiency, and altitude. The applied
fixes: subjectLimit now lives once in regexp.go next to the int32
rationale and Exec uses it; the matcher workspace arithmetic moved
into workspaceHeapBound in engine.go beside prepare, so both change
together; the compaction threshold became the named queueCompactFactor
constant, which the heap bound and the queue test both derive from;
the onq scratch shrank to a byte per instruction as a bool slice;
astSize reuses the walk helper; solverDepth merged its duplicated
child-max loops; the magic 17 in bracketAtomCost got a name and an
explanation; and a new size-constant test anchors every mirrored
64-bit layout (ptree, the memo records, Match, Error, slotTable,
engineWS, decoded) with unsafe.Sizeof, so a grown type fails a test
instead of silently shrinking a contract. Cross-comments now point
from the measured types to the contract. The tests compile each
pattern once instead of per size, and the compaction unit test uses
slices.Equal. Skipped on purpose: exposing the contract as a Regexp
method (the goal asks for a Compile variant), removing the defensive
OnePass branches in the aggregate accessors, and precomputing the
pattern-independent preimage product, which reads better as a loop.
Full tests, the race detector, the 386 vet, and the benchmarks all
pass unchanged.

**Request:** in go0, add a ReplaceAll function and a MatchAll function
with a callback for every match.

**Done:** added go0/replace.go with both entry points. MatchAll scans
the subject left to right and calls the callback with an absolute,
reused pmatch slice; false stops the scan. It follows the sed global
rule: the next search starts at the match end, a null match there is
skipped, and a counted null match advances one character. Restarts
clear NotBOL only after a newline under the Newline flag, so anchors
keep their meaning mid-subject. ReplaceAll builds on MatchAll with a
sed-style replacement text: & is the whole match, \1..\9 are groups
(parsed once into parts), backslash escapes the next character, and a
reference past NumSub reports ESubReg. Both refuse NoSub expressions.
Tests cover spans, groups, early stop, anchors, multibyte null-match
advance, replacement escapes, and the error paths. README documents
the API. Full suite, vet, and gofmt pass.

**Request:** also add a ReplaceAllFunc variant, like Go's regexp.

**Done:** ReplaceAllFunc takes a callback that receives the same
pmatch slice as MatchAll and returns the replacement text, inserted
literally. The rewrite loop moved into a shared replaceAll helper
that ReplaceAll and ReplaceAllFunc both use. Tests cover the literal
insertion, group access, and the NoSub refusal. README updated.

**Request:** rename MatchAll for consistency with the Func naming.

**Done:** renamed it to MatchAllFunc, which mirrors ReplaceAllFunc:
All says it visits every match, Func says it takes a callback. Plain
MatchFunc would read as a predicate variant of a Match method. The
rename covers replace.go, the tests, and the README. Tests, vet, and
gofmt pass.

**Request:** reconsider the names against the PCRE preg_* set, or
idiomatic Go.

**Done:** settled on MatchAll, ReplaceAll, and ReplaceAllFunc. The
replace pair uses the exact Go stdlib regexp names, which also map
one-to-one onto preg_replace and preg_replace_callback. MatchAll
mirrors preg_match_all and the original goal. No separate Match
function: Exec already plays the preg_match role, and Match is
already the offsets type. This reverted the MatchAllFunc rename.
Full tests, vet, and gofmt pass.

**Request:** add an optional match limit to MatchAll, ReplaceAll, and
ReplaceAllFunc, and bring all documentation up to date.

**Done:** each function now takes a limit int before the flags, like
the preg_replace limit and the n of Go's FindAll. Negative means no
bound, zero reports nothing, and a skipped null match does not count.
With the replace functions, the subject past the last counted match
stays unchanged. Tests cover limits on all three functions. The
README section shows the new signatures, NOTES.md lists the global
operations, and a wrong preg_match_all mention in a doc comment
became preg_replace. Full tests, vet, and gofmt pass.

**Request:** /simplify pass over the ReplaceAll/MatchAll commit.

**Done:** four parallel review agents looked at reuse, simplification,
efficiency, and altitude. Applied: the restart boundary rule moved
into continuationFlags in engine.go, next to bolAt, so the newline
logic has one home; the NoSub refusal got a dedicated ENoSub code
instead of misusing BadPat; the null-match advance now uses the
package decodeRune helper; the limit parameter decrements in place,
dropping the count variable; parseReplacement flushes literals in one
place, lost the group -1 sentinel, and preallocates; replaceAll grows
its builder once, on the first match. Tests now use slices.Equal and
compileOK, check the ENoSub code, and hoist a repeated subject.
Skipped: a search-from-offset engine primitive (too large for this
pass), pooling the pmatch slice (unsafe with user callbacks), and
folding limit into the callback (the counter is the single shared
spot already). Full tests, vet, and gofmt pass.
