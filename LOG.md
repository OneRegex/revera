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
