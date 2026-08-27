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

**Request:** rewrite the go0/ ERE engine in a simplified Go subset in
go1/, with a full specification of the subset, and a tool that
translates subset programs into JSON, so that Rust, C++, and Zig
implementations can later be generated from the JSON and the JSON can
be proven correct with LEAN4. Keep progress in a markdown file.

**Done:** the subset is named Vego and specified in
go1/VEGO-SPECIFICATION.md: fixed-width integers, immutable strings,
slices with a single-owner buffer model, structs, free functions, no
pointers in data, no maps, no closures, no imports. The engine lives
in go1/revera as one flat package, locale handling included; trees
became index arenas, the memo maps became open-addressing tables
written in the subset, callbacks became a MatchIter iterator, and
go:embed moved to a *_host.go shim. go1/cmd/vego2json checks the
subset with go/ast and go/types and emits the JSON form
(go1/revera.vego.json); go1/cmd/json2go regenerates Go from the JSON
as the reference converter. Differential tests compare go1 with go0
over locales, UTF-8, random and fixed pattern corpora, replacement,
and iteration; the regenerated-from-JSON engine passes the same
suite. Findings live in go1/PROGRESS.md. Tests, vet, and gofmt pass.

**Request:** (continued) review the go1 work with swival and fix
what it finds.

**Done:** swival reported six findings and I fixed all of them.
LocaleOpen now rejects every malformed blob through a full
cross-section validation pass instead of panicking, with a
corruption test. The vego2json checker now enforces pointer types
as parameter-only, & as a direct call argument only, no slice-typed
or sliceable globals, uint8-only string conversions, safe handling
of malformed range statements, and a minimum append arity; the
specification text matches. The JSON was regenerated and the
regenerated-from-JSON engine passes the full differential suite
again. Tests, vet, and gofmt pass.

**Request:** /simplify pass over the go1 work.

**Done:** four parallel review agents looked at reuse,
simplification, efficiency, and altitude. Applied: the vego2json
global rules now go through one globalBase helper (closing paren,
nested-index, and address-of-field gaps), its operator tables are
package-level, and the break stack is boolean; the capture solver
seeds one scratch arena slot so the failure paths lost their
per-allocator dances, kid windows grow in bulk without a dead fill,
and both head-plus-tail sites share kidPrepend; locale loading split
into LocaleLoad and LocaleSelect so the host Open validates the
embedded blob once; a shared indexOfByte replaced four hand-rolled
scans; the preimage dedup uses runesContain; dead code went away
(validUTF8, LocaleIsPOSIX, the localeIndex field); CollatingPrefix
is exported again for go0 parity; runPhaseA lost its pooling-era
wrapper; ReplaceAllFunc now drives MatchAll; json2go merged its
const/var emitters and picks conversion parens by JSON kind; the
tests dropped their hand-rolled UTF-8 decoding for the stdlib and
share a g0Code helper. Skipped: merging minMatchChars into
computeLengths (different saturation caps; merging would change the
oversized-pattern fallback against go0 on multi-gigabyte subjects)
and removing the Regexp.multi field (inherited go0 shape, read once
at compile time). The JSON was regenerated; the full differential
suite and the regenerated-from-JSON engine both pass, with vet and
gofmt clean.

## Target printers and instantiations (Zig, C++, Rust)

The user asked, based on the work in go1, for a printer and
implementations in Zig, C++ and Rust, verified correct. I built a
shared front end, go1/vegoc, that loads revera.vego.json, infers
the type of every expression, folds constants, and computes local
usage and mutation; three printers (cmd/json2zig, cmd/json2cpp,
cmd/json2rust) consume it. Each target directory (zig1/, cpp1/,
rust1/) pairs the generated engine with a hand-written minimal
runtime: Slice and Str value types with Go slice-header semantics
lowered to pointer plus length (the spec's sanctioned route, since
Vego views can alias), Go-exact integer conversion and comparison
helpers, the embedded locale blob, and three arenas (persistent
locale data, per-pattern, per-operation scratch) replacing the
garbage collector. Verification is a cross-language differential
harness: revera/driver_host.go defines a line protocol, each
target ships a driver speaking it, and cmd/crosscheck feeds all
drivers the corpora of the go1 differential tests (random patterns
across every flag set, the fixed corpus, the cs multi-element
locale, replacement, iteration, contracts, and locale case-map
sweeps), comparing against the Go engine line by line. The final
joint run covers 191059 commands, including 25000 extra random
patterns; the Zig, C++ and Rust drivers agree with Go on every
line. Language lessons landed in api-faq.md, design notes in
go1/PROGRESS-TARGETS.md, and missteps in MISTAKES.md.

## Review round on the target printers

A swival review of the finished targets surfaced translator gaps
and two latent engine-relevant bugs, all fixed and re-verified.
The deep one: Go zeroes allocations and the engine extends slices
inside capacity, so the C++ and Zig runtimes had to zero the spare
region of grown buffers; malloc had been handing out zero pages by
luck, and poisoning the region proved the dependence. The C++
printer now pins side-effecting operands and arguments into
ordered temporaries, since C++ leaves evaluation order unspecified
where Go fixes it left to right; pointer-typed arguments stay
inline because they lower to references and a temporary would copy
the referent. Signed division goes through MinInt-safe helpers in
all three targets. Smaller lowerings landed for []uint8(s), &^= in
C++, MinInt literals, partial array literals, struct equality over
string and array fields, and range statements. The driver protocol
gained error positions on replacement, both O endpoints truncate
like the reference, and the corpus now sweeps execution flags over
replacement and iteration and runs case-insensitive matching per
locale. A new go1/probe package exercises every previously
uncovered construct; cmd/probecheck verifies the three probe
binaries against the Go package, and all match. Final state:
191689 corpus commands and 24 probe lines agree across Go, Zig,
C++ and Rust.

## Second review round on the target printers

A re-run of the swival review returned four findings, all fixed:
package variables may not contain slices at any depth (vego2json
and vegoc both reject them now, since globals are static constant
data no target can allocate); the Rust printer accepts subslice
views as assignment bases (a slice base is a value, not a place);
Rust &^= routes through the same pinned-place lowering as other
compound assignments, keeping Go's place-before-value order; and
the C++ zero-length array view uses a static sentinel instead of
arithmetic on a fabricated pointer. The probe suite grew to 29
lines covering each fix, plus unsigned make lengths and indexing
an array-typed call result, which earlier rounds fixed. All
corpora and probes pass in the three languages.

## Simplify pass on the target-printer change set

Four parallel review agents (reuse, simplification, efficiency,
altitude) went over the hand-written files. Applied: the corpus
generators and fixed tables moved into revera/corpus_host.go, so
the differential tests and crosscheck share one source (their
copies had already drifted); vegoc gained WalkExpr/WalkStmt and
the six hand-rolled AST walkers now use them, with the printers'
diverged containsCall copies replaced by vegoc.Impure; LoadFile
and TupName joined vegoc; the two-value-assign side-effect rule
moved from three printers into the checker; array lengths resolve
once during Check, replacing the foldProgram package global whose
answers depended on whether a check was in flight; crosscheck runs
its drivers concurrently and imports the engine's flag constants;
the runtimes zero only the spare region of grown buffers and bulk-
copy slice literals; the C++ driver decodes hex without sscanf;
and a dozen small cleanups (dead assignments, one-caller helpers,
duplicate constants, redundant length checks, std.mem.zeroes and
the {x} formatter in Zig). Skipped by choice: bump-allocator
arenas for C++/Rust (a verified-memory rework for milliseconds),
moving the probe call matrix into the subset (a redesign, and the
per-language mains are deliberate differential surface), and
deriving HasElse from nil (the flags document loader intent).
Everything reverified: full test suite, the 191689-command
extended crosscheck, and all 29 probe lines in three languages.

## LEAN4 proof of the Vego pipeline

The user asked for the correctness of the vego code, through its
JSON representation, to be proven with LEAN4, fixing any real vego
defect the proof might surface at the root. A new lean/ directory
holds the model: a total JSON decoder into a raw AST, an
elaborator into a fully typed core (resolved names, wrapping
widths, exact untyped-constant folding, explicit zero fills), a
total heap-based operational semantics with traps for the
specification's abnormal terminations, and host harnesses that
mirror probe_host.go and driver_host.go. crosscheck gained a
-dumpexpected flag so the exact corpus with the Go engine's
outputs feeds both the Lean theorems and the vegocheck replay
binary. Machine-checked results: both shipped artifacts are well
formed; the probe program reproduces all 29 reference lines; the
revera engine answers the crosscheck corpus exactly like the Go
engine through the same driver protocol, trap-free, under an
explicit per-command fuel budget. No defect in the vego artifacts
surfaced; the two bugs the differential runs caught were in the
Lean elaborator itself and were fixed there.

## Review round on the LEAN4 model

Profiling with sample(1) exposed that Lean's borrow inference made
every nested buffer write copy its containing array (the top stack
entry was lean_copy_expand_array); inlining the short write paths
into M.writeLoc with a detached slot took the worst corpus command
from 104 seconds to 0.4 and the heavy fixed-pattern block from 412
seconds to 1.65. A swival review of the whole change set returned
five findings. The important one was a real gap in the pipeline
itself: cap() after a growing append is observable in the subset,
Go grows with its runtime policy while the Zig, C++, Rust runtimes
and the Lean model allocate max(2*cap, 8, need), so the value
differs between targets. The specification now declares post-growth
capacity target defined and requires that it never reach observable
output; the engine's single post-growth cap read only selects
between equivalent paths, which the cross-target corpus verifies.
The review also caught a missing int32 narrowing of the O command
bounds in the Lean driver session; the narrowing is fixed and the
corpus now carries two out-of-range O commands so every driver
proves it. The remaining findings were documentation wording about
the fuel bound, which is a per-call recursion-depth bound, not a
work budget; the texts now say so.

## The theorems check

Two memory rounds stood between the model and the finished proof.
The theorem evaluation first ran in Lean's IR interpreter, because
lake does not precompile user modules by default; sample(1) showed
lean::ir::interpreter on top and precompileModules = true fixed it
(the finding is in api-faq.md). The monster automaton executions
then ran out of memory, because the interpreter never freed the
frame cells of the millions of calls inside one command. The heap
now recycles cells through a free list, every call frees its frame
on return, and each cell carries a generation, so a view or borrow
that illegally outlives its call traps as stale instead of reading
recycled memory; the buffer model's lifetime discipline is now
enforced dynamically. With that, lake build completes: both JSON
artifacts are well formed, the probe program reproduces all 29
reference lines, and the revera engine answers all 86691 corpus
commands exactly like the Go engine, trap free. Each theorem
depends only on propext, Classical.choice, Quot.sound and its
native_decide axiom. The proofs live in their own lake target, so
building the vegocheck tool no longer replays them.

## Simplify pass on the LEAN4 model

Four parallel review agents (reuse, simplification, efficiency,
altitude) went over the Lean sources and the crosscheck change.
Applied: the corpus replay policy now lives in one corpusStep
function that both runCorpusFuel and the vegocheck loop fold over,
with parseCorpus shared the same way; the Session resolves every
protocol function index and struct field position once at start
(the O digest alone was re-scanning 195 function names twice per
rune); Session.call1/call2 replaced fourteen hand-written arity
matches; Machine.call now enters the interpreter through the same
runFn as in-language calls; append, copy and the byte conversions
use bulk buffer reads and writes instead of per-element cell
round-trips; byteCompare, strCompare and applyCmp moved to the
core so the elaborator's constant folding shares them; the dead
probeProg/reveraProg pair left Data.lean; the probe harness lost
its m1..m32 numbering; hex encoding uses Nat.digitChar and decodes
without a char list; and a dozen smaller cleanups (List.lookup,
refutable let patterns, merged match arms, dropped unused
constructor fields, an always_inline monad instance). Skipped by
choice: hoisting the per-arm fuel matches (the boilerplate is the
price of first-order termination, and a forty-arm rewrite of the
verified interpreter risks silent fuel drift for a cosmetic gain),
the path-list avoidance in place resolution and pre-evaluated
switch tables (deeper interpreter rework than the payoff), and an
opaque reference type for the generation tags (a violation already
traps deterministically). Probe, edge, heavy-segment and 3000-
command checks pass on the refactored code; the theorem build and
the full replay were relaunched to re-establish the proofs.

## Thread safety: no globals, explicit memory contexts

The user asked to make the generated engines thread-safe: no
global variables, no global allocators, with the root cause and
the specification fixed, and API changes allowed. The old scheme
kept three arenas as globals in each runtime behind a mode switch
the drivers flipped; Rust even used `static mut`. The fix makes
memory explicit end to end. vegoc now computes a transitive
per-function Allocates flag (make, append, the two string
conversions, slice composite literals, plus calls to allocating
functions) and reserves the identifier `mem`. Each printer gives
every allocating function a synthetic first parameter `mem` and
threads it through call sites: a `std.mem.Allocator` in Zig, a
`vg::Arena&` in C++, and a `&vg::Arena` in Rust, where the block
list sits in an UnsafeCell so Arena is !Sync and cross-thread
sharing is a compile error. The runtimes lost all their state,
including the C++ zero-length-array sentinel (the view now points
at the array object itself). The drivers own the three arenas as
locals in main, pass the right one to each call, and moved their
own file-scope state (locale, compiled pattern, buffers) into
main. Spec section 9.1 documents the scheme; the READMEs and
PROGRESS-TARGETS.md follow. The Vego JSON is unchanged, so the
LEAN4 artifacts stand. Verified: go tests, probecheck (29 lines,
all targets) and the full crosscheck (86691 commands, all three
drivers agree with the Go engine).

## Simplify pass on the memory-context change set

The user ran /simplify over the thread-safety diff. Four review
angles (reuse, simplification, efficiency, altitude) ran as
parallel agents. Applied: the callee-allocates lookup moved into
vegoc as Program.CalleeAllocates, used by all three printers; the
allocation-site predicate became the exported vegoc.ExprAllocates,
now also used to reject allocating package-level initializers
(they would reference a memory context that does not exist at
global scope); a new vegoc test pins every allocation form to the
Allocates flag, the transitive propagation, the reserved name and
the initializer rejection; vg.rs dropped the dead unsafe Send and
Sync impls (only Sync for Str stays, for the generated static
tables); the Zig Host lost its rows buffer (the 'I' handler takes
it from the scratch arena) and keeps the persistent arena as a
plain allocator; the Rust driver gained the same three-arena
header comment as the other two. Skipped: a MemName constant (the
name sits inside per-language format strings; the new test guards
the drift) and reverting the C++ line buffer to a function-local
static (that is global storage again). Regenerated engines are
byte-identical; probecheck and a quick crosscheck re-verify the
rebuilt Zig and Rust drivers.

## Resource contracts, proved in LEAN4

The user asked for a LEAN4 proof that the contract computation is
universally correct, or at least that a real evaluation never
exceeds the contract. The work has two layers, because the second
is what a deep embedding can reach and the first is what it
cannot.

The first layer is a resource meter inside the formal semantics.
The heap of Interp.lean now carries five counters: the bytes of
every buffer allocation, the abstract steps, the loop and call
count, the live call depth and its running maximum. Allocations
are charged at the fixed 64-bit layout that the Zig, C++ and Rust
targets share, computed by a layout function that aligns struct
fields the way Go does. Charges sit at exactly the allocation
forms that vegoc marks: make, a growing append, a slice literal,
and the two string conversions. To make each one provable, the
allocation cores moved out of evalExpr into named functions
(doMake, doSliceLit, doStrToBytes, doBytesToStr), and freeFrame
and rebindSlot became tail recursive.

The driver session then measures each Exec the way an application
would: it asks the engine's own interpreted ContractFor for the
figures at that subject length, resets the meter, runs the call,
and compares. Passing ContractHeapBytes, ContractStackBytes at the
contract's 256-byte frame estimate, or ContractSteps is a hard
session fault. The corpus theorem, renamed
revera_corpus_agrees_within_contract, therefore states agreement
with the Go reference and contract compliance together. A first
draft counted every executed statement against ContractSteps and
three tiny-subject commands passed the figure by 1.2x; the check
now counts loop iterations and calls, which is the granularity
contract.go describes, and the statement counter stays for
calibration. vegocheck grew a --contracts mode that reports the
tightest margins per meter instead of failing, and reports them
during the replay because a few corpus patterns run for hours.

The second layer answers the universality question honestly. Two
new modules prove statements that quantify over all inputs, by
induction, with no native evaluation. CostLemmas.lean covers the
cost model: the geometric bound of the portable growth rule
max(2*cap, 8, need), its connection to real append histories, the
well-formedness of the layout function, the exactness of the meter
on every allocation form, and the saturation algebra of cAdd and
cMul. MeterSound.lean covers the interpreter itself: one mutual
induction on fuel over all sixty evaluation cases proves that no
counter ever decreases and that the call depth returns to its
entry value whenever a call completes, up to callIdx_meterOK for
harness calls. That makes the driver's reset, run, read protocol
sound by proof rather than by inspection.

What stays corpus bound is the engine's own control flow. A fully
universal contract theorem would need verified invariants for the
matcher, the one-pass walk and the parse solver, which is a
separate project; the READMEs now say so instead of implying more.

Measuring the replay changed the plan for the corpus theorem. The
corpus is not uniformly cheap: twelve compile blocks, six of
`((a*){250}){250}b` and six of `((a*){4}){4}`, cost hours between
them, while the other 85599 commands replay in about a minute. The
old README promise of "tens of minutes" for the whole theorem was
wrong by an order of magnitude. So `Vego/Corpus.lean` now derives
the corpus without those twelve blocks from the same embedded
data, and a second theorem states the same contract claim over
that 98 percent in minutes. The proposition re-checks its own
coverage, so a filter that matched everything or nothing would
fail the theorem instead of quietly weakening it. The full theorem
stays as the complete claim.

Verified in this session: the three cheap theorems still pass
against the refactored interpreter, including the 29-line probe
matrix that pins spare-capacity zeroing and evaluation order; and
`vegocheck` replayed 85599 corpus commands with contract
enforcement on, all agreeing, none exceeding its contract. The
tightest margins over the first 3000 commands were 50 percent of
the heap bound, 78 percent of the stack bound and 22 percent of
the step bound, so the contracts hold but the stack figure has the
least room.

Calibrating over the whole quick corpus (69248 measured Exec
calls) found one case with no headroom at all. The worst heap
margin is 60 percent of the bound and the worst loop margin 27
percent, but the worst stack margin is exactly 100 percent:
pattern `[[=ch=]]` on subject "HhhxhH" reaches 18 interpreted call
frames against a bound of 18 frames. That is not an accident of
the model. `matcherContract` prices phase A as matcherStackBytes,
2048 bytes or eight frames, plus equivFrames, which is
maxElemAhead + 2 = 10 frames, and the multi-character equivalence
test really does recurse once per character of the collating
element. The contract holds, since only a run that passes the
bound is rejected, but nothing is spare: one more frame anywhere
on the phase A path would break it. Whoever touches that path
should raise the equivFrames slack first.

The two corpus theorems then moved apart, into `Vego/Theorems.lean`
and `Vego/TheoremsFull.lean`, with a `FullProof` Lake target for
the second. One module cannot cache half a proof, so keeping both
statements together meant nobody could check either without paying
for the slow one. Now `lake build` finishes in about two minutes
and checks four theorems: both well-formedness claims, the probe
agreement, and the quick corpus contract theorem over 85599
commands. `lake build FullProof` states the complete claim and
runs for hours.

The user then asked to stop the full replay and reduce the corpus
theorem to sensible cases, so that it runs quickly. Measuring the
two intractable patterns settled how. The cost comes from the
nesting, not the subject: `((a*){250}){250}b` needs about a minute
on the empty subject and 107 minutes on a 120 byte one, and
`((a*){4}){4}` needs minutes even on the empty subject, so no
subject length is cheap for either. Each of the twelve blocks
holds four 120 byte subjects. A full replay would take days.

So the theorem now drops exactly the executions of those two
patterns, 1056 X commands, and keeps everything else. All 9779
compile commands stay, so no pattern goes unchecked, and the T
commands of the intractable blocks stay too, so the contract
figures of the extreme patterns are still compared against the Go
reference. Those are the figures that reach the saturation cap, so
they are the ones worth keeping; only the executions go. Dropping
an X command cannot disturb the session, because it allocates its
own match buffer and writes no session root. The proposition
re-checks its own coverage: it fails if the filter ever stops
keeping every compile, or matches everything, or matches nothing.
`TheoremsFull.lean` and the `FullProof` target are gone, and one
`lake build` now checks all four theorems in four minutes and
forty-six seconds.

## Simplify pass on the contract proofs

The user ran /simplify over the contract commit. Four review
angles ran as parallel agents, and two of them found errors in the
prose rather than the code. The claim that "every Exec call stays
within its contract" was too strong: ReplaceAll and MatchIterNext
call Exec inside the engine, so the 360 R and I commands run Exec
calls the session never meters. The claim that the intractable
patterns' figures "reach the saturation cap" was simply false;
they report about 6.6e12 steps against a cap of 4.6e18. Both are
corrected, and the theorem now says what it checks.

Applied to the code: the append growth rule moved into
`Interp.lean` as `growCap`, so the cost lemmas reason about the
definition the interpreter runs instead of a copy that could
drift; `Session.compact` keeps the meter instead of rebuilding a
Heap literal that silently reset it; the T command and the
per-Exec check now share one `contractOf`, which also gives T the
cache; `dropSlowExecs` became tail recursive, which the Lean
interpreter had refused to evaluate at 86691 frames; the corpus
filter tokenizes each command once through a `CmdKind` rather than
three times; `enforce` and `collect` collapsed into one
`calibrate` field, since they were always complementary; the
margin report labels the step counter "reported only", because the
session enforces the loop counter; and two dead lemmas went, along
with local re-implementations of `List.sum` and `List.getLastD`.

Skipped: about 430 lines of duplicated tactic script in
MeterSound.lean, where the four `assignCall2` branches, the three
slice-bound blocks and the loop-body tails repeat. The
duplication is real, and factoring it is worthwhile, but these are
brittle scripts against do-notation join points and each attempt
costs a five minute rebuild to verify. Also skipped: reading
`frameBytes` from the artifact's own constants instead of
hardcoding 256, which needs the elaborator to carry its folded
const environment onto TProgram, and grouping the meter fields
into a nested record, which touches every one of the fifty MOK
lemmas. Both are worth doing and neither is cleanup.

## Idiomatic public APIs for the four targets

The user asked for public interfaces that look idiomatic and are
simple to use, in the generated Go, C++, Rust and Zig
implementations.

The generated engine was the whole surface until now. It exports
every internal function, asks the caller for an arena, returns
integer error codes, and names things after the Vego package. That
shape is right for a translator and wrong for a programmer. So
each target now carries one hand-written file above the engine.
The engine did not change; only C++ needed a printer flag, and the
Vego JSON is untouched, so the LEAN4 proofs stand as they were.

Go got the surface back in `revera_host.go`. `New` returns
`(*Regexp, error)` and `MustNew` panics for build-time patterns.
The options are functions: `CaseInsensitive()`,
`NewlineSensitive()`, `NoCaptures()`, `ShortestMatch()` and
`In(loc)`. The methods copy the standard `regexp` package names,
so `MatchString`, `FindStringSubmatch`, `FindAllStringIndex`,
`ReplaceAllString` and the rest all read as a Go user expects.
`Error` implements `error`, and `Contract` grew `HeapBytes`,
`StackBytes` and `Steps` methods. The old ad-hoc wrappers `Open`,
`MatchAll`, `ReplaceAllFunc` and `CompileWithContract` are gone.

Rust got `src/lib.rs`, and the crate is now a library with the two
drivers as its binaries. `Regex::new` and `RegexBuilder` compile;
`find`, `captures`, `find_iter` and `captures_iter` search. A
missing match is `None` and only a real failure is `Err`, so the
signatures read `Result<Option<Match>>`. `Match` borrows the
subject and answers `as_str`, `range`, `start` and `end`.
`Captures` indexes with `[]` and panics on a group that took no
part, exactly like the `regex` crate. `Error` implements
`std::error::Error` and carries an `ErrorKind` and a byte offset.

`Regex` is `Send` and `Sync` in Rust. The claim rests on three
facts: nothing writes the arena after `build`, every search copies
the header it walks, and every allocation a search makes goes to
an arena that search owns and frees. The first fact was checked
against the Go source; only `Compile` and its helpers write the
`Regexp` or its nodes.

Zig got `src/revera.zig`, exported by `build.zig` as the module
`revera`. `Regex.compile` takes an allocator, the pattern and an
options struct with default fields. Failures are a plain error
set, and `Options.error_position` receives the byte offset.
`matches` and `captureMatches` return iterators with `next`. Each
iterator step owns its scratch arena and frees it, so no iterator
needs a `deinit` and no caller has to remember one. Zig is the one
target whose thread claim carries a condition: a search allocates
from the allocator the caller gave `compile`, so that allocator
must be thread safe.

C++ got `revera.hpp` and `revera.cpp`. The header includes
standard headers only: the engine sits behind a `unique_ptr`, so
`vg.hpp` and the arenas never reach a caller. `revera::Regex`
searches; `find` and `captures` return `std::optional`, and
failures throw `revera::Error` with a `Failure` code and the
offset. `Options` is a designated-initializer struct, so
`revera::Regex re("ab+", {.case_insensitive = true});` works.
This is the one target that needed a printer change: C++ has a
single namespace mechanism for both levels, so `json2cpp` took a
`-ns` flag and the generated engine moved to
`namespace revera::engine`.

The execution flags of `regexec()` stay off all four surfaces.
`REG_NOTBOL` and `REG_NOTEOL` matter for scanning a buffer in
pieces, and the iterators already handle that case themselves. A
caller who still wants them drops to the generated engine, which
every target keeps reachable and documents.

Each target gained a test file for its API: `api_test.go`,
`tests/api.rs`, `src/revera_test.zig` and `api_test.cpp`. All four
cover the same ground, including a thread test that exercises the
shared-search claim. The C++ and Zig builds gained a `test` target.

Verification: the full Go suite passes, `cargo test` passes with
its two doctests, `zig build test` passes ten tests, `make test`
passes in cpp1, and the full crosscheck replays 86691 commands
against all three drivers with no disagreement. Spec section 9.2
records how each target separates the two levels.

## Simplify pass on the public APIs

The user ran /simplify over the API change. Four review angles ran
as parallel agents, and the most valuable finding was a measured
one. The C++ `find_all` gave the whole iteration a single arena,
and `vg::Arena` frees only when it dies, so every match's search
workspace stayed alive until the scan ended. A 20000-match subject
peaked at 167 MB. One arena per step, with the match slice kept in
an outer arena, brings the same call to 3 MB. Rust and Zig already
built a fresh arena per step, so C++ was alone in this.

Three findings were wrong and stayed unfixed. Two agents proposed
allocating fewer match slots for whole-match iteration, but
`MatchIterNext` refuses a slice shorter than `NumSub()+1` and
reports `ESpace`. One agent read a `defer` inside a Zig loop body
as deferring to the end of the function; Zig runs it at the end of
each iteration.

What the pass changed, beyond the arena fix. In Rust, `Contract`
became a plain struct with public fields, which is what C++ and
Zig already had, and that removed seven accessors and a
hand-written `Debug`. The `Scan` tri-state became an enum, and the
832-byte header copy moved out of the per-step path into the enum,
where one copy serves the whole scan. `exec` and `step` now take a
closure, so a search for one span no longer builds a `Vec` for it.
`ErrorKind::NoMatch` went, because the engine never returns that
code and the other two targets had already left it out.

In Zig, the `Scratch` type was `std.heap.ArenaAllocator` under
another name, so it went and the six call sites use the stdlib type
directly. The `gpa` field duplicated `arena.child_allocator`, so it
went too. `clamp` was `std.math.lossyCast`. The test file also lost
fourteen `@as(usize, ...)` wrappers: `expectEqual` resolves the
literal against the value it is compared with, so the casts said
nothing. The casts that remain, in `revera.zig`, `main.zig` and
`vg.zig`, are all conversions the compiler demands; removing the
one in `Str.sub` fails with "@intCast must have a known result
type", because pointer arithmetic gives it no result type.

In Go, four `FindAll` methods repeated the same eight-line
scaffold, and a generic `collect` replaced it. A `search` helper
now holds the `FlagNoSub` rule that had been written twice and
omitted four times. `texts` reads the match spans directly instead
of routing them through a throwaway `[]int`. `ReplaceAllStringFunc`
uses `strings.Builder`, which drops the final full copy that
`string(out)` was making. The Go suite also gained the concurrency
test the other three targets already had; it passes under `-race`.

Both runtimes gained the two helpers the API layers had been
writing for themselves: `str`/`view` to wrap caller bytes as a
`Str`, which `vg.zig` already had, and `str_dup` to copy a string
into an arena. That removed a `reinterpret_cast` from the C++ layer
and a hand-built `Str` from the Rust one.

The Rust driver went back to its own `mod engine; mod vg;`. Using
the library put the differential driver, which is what proves the
engine agrees with Go, downstream of the convenience layer; a
broken `lib.rs` would have stopped the verification build.

Skipped, with reasons. Moving the `FlagNoSub` guard into the Vego
source would fix it once for all four targets and put it under the
LEAN model, but it changes `revera.vego.json` and needs every
engine and the proof artifacts regenerated, which is well outside
this change. Carrying doc comments through the JSON so each printer
can emit them is the root fix for several duplication findings, and
it is a project of its own. The `-ns` default for `json2cpp` is
still the package name, which is now wrong for cpp1; a regeneration
that forgets the flag collides with `revera.hpp` and fails to
compile, loudly, so the Makefile is adequate. The lossy UTF-8
conversion in the Rust layer stays lossy: the input is always
valid, and a panic would be worse than a replacement character.

Verification after the pass: the full Go suite, `cargo test`,
`zig build test`, `make test` in cpp1, and the full crosscheck over
86691 commands against all three drivers, plus probecheck. The
comment edit in `replace.go` is a subset file, so `vego2json` ran
again and the JSON came back byte-identical.

## Comment pass over every source file

The user asked for one sweep over the whole tree: drop the code
comments that only repeat the code, make the rest say why a thing
exists rather than how it works, write them in ASD-STE100, use no
em dash, stop wrapping at a column, and break the line after every
long sentence or paragraph.

The last two rules changed the shape of almost every comment in
the repository. The old style wrapped prose at about seventy
columns, so one sentence spanned three or four lines and two
sentences shared a line. The new style puts one sentence on one
line, whatever its length. A diff of a comment now shows which
sentence changed, and nothing else. The paragraph breaks stay.

The prose changed with the layout. ASD-STE100 wants one idea per
sentence and simple tenses, so the semicolons and the explanatory
colons that had been joining two facts became separate sentences.
Dozens of them, across Go, Rust, C++, Zig and LEAN4. A few passive
constructions became active where the actor was obvious: "the
queue gets compacted" is now "the queue compacts", and "a blob
that fails any check is rejected" is now "the loader rejects a
blob that fails any check".

The house rule of twenty words per sentence took a second pass. A
count over the finished tree found eighty-three sentences above
it, thirty-eight in the four target languages and forty-five in
the LEAN4 model. Most held two facts joined by "so" or "and", and
split cleanly. Four survive, and each one is a single idea with a
list inside it, where a split would only scatter the list.

What I removed. The four edge flags of the `decoded` window in
`go0/oracle.go` and `go1/revera/match.go` each carried a line that
spelled the field name out in words, and the type comment above
them already says the flags carry the anchor context. The
`inRanges` and `bracketInRanges` comments said the function tests
range membership, which is the name, plus "with a binary search",
which is the implementation. Both went. That is the whole list:
the rest of the comments in this tree earn their place, and the
doc comments on the public API of the four targets stay even where
they read as obvious, because they are the rendered documentation.

The LEAN4 files needed a script. Their block comments have no line
prefix, so a reflow had to find each block, keep its delimiters,
join the prose paragraphs and split them at sentence boundaries,
and leave the indented code samples and the numbered lists alone.
I hand-wrote the lists afterwards, because a bullet that ends in a
semicolon is not a sentence.

Verification. Every target builds and passes: `make test` for the
C locale runtime, the full Go suite in `go0` and `go1`, `cargo
test`, `zig build test`, `make test` in cpp1, and `lake build` for
the LEAN4 model. `vego2json` ran again over both subset packages
and the two JSON artifacts came back byte-identical, so the
comment edits inside the subset changed nothing the printers can
see. Regenerating all six engine files from that JSON reproduced
them byte for byte. probecheck passes against the three target
probes, and crosscheck replays all 86691 commands against the Zig,
C++ and Rust drivers.

The `swival` review did not run. Its provider returned "servers are
currently overloaded" on every retry, twice, so I read the diff
myself instead. The read found five slips of my own and fixed
them: `clamp` saturates rather than clamps, the loader is what
rejects a malformed blob, a rename covers every local and not only
a renamed one, one printer sentence had left its verb without a
subject, and "this branch cannot happen" said "branch" in a file
where a branch is an alternation arm.
