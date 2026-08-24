# Implementation techniques from the reference engines

Status: source study performed 2026-08-24.

## Scope and baselines

This document extracts implementation techniques that can help implement
[`POSIX-1-2024-ERE-SPECIFICATION.md`](POSIX-1-2024-ERE-SPECIFICATION.md). It is
not a claim that any reference implements that contract in full. The inspected
revisions are:

- TRE `71bfcaf0af3994384987c6c2679ed7d078ffe189`;
- RE2 `972a15cedd008d846f1a39b2e88ce48d7f166cbd`; and
- MinRX `d13610cdf983337d32b5e07a46da69e40ec5adb0`.

| Reference | Most useful ideas | Semantic boundary |
| --- | --- | --- |
| TRE | Tagged TNFA, arenas, start filtering | Known Issue 8 gaps |
| RE2 | Sparse sets and executor fast paths | Different syntax, locale, captures |
| MinRX | Structured NFA and compact path state | Unproven; collation gaps |

The central rule is to optimize a proved-equivalent representation, not to
adopt a reference engine's observable choices. In particular, RE2 deliberately
uses backtracking-style submatch priority in its NFA
([`re2/nfa.cc`, lines 7-22](../ref/re2/re2/nfa.cc)), and MinRX says that its
structured-NFA algorithm is not yet proven correct
([`ALGORITHM.txt`, lines 435-451](../ref/minrx/ALGORITHM.txt)). TRE's known
Issue 8 gaps are recorded separately in
[`TRE-POSIX-ERE-DIVERGENCES.md`](TRE-POSIX-ERE-DIVERGENCES.md).

## Separate the immutable program from execution state

Compile the pattern and compilation-time locale into an immutable program.
Keep active states, tag vectors, decoder state, and temporary queues in a
per-`regexec()` workspace. This gives concurrent calls no shared mutable match
state, as required by Section 13.3 of the specification.

TRE's compiled object holds transitions, tags, submatch instructions, first
characters, and flags, while its parallel matcher allocates a private workspace
for each call ([`lib/tre-internal.h`, lines 241-263](../ref/tre/lib/tre-internal.h);
[`lib/tre-match-parallel.c`, lines 155-205](../ref/tre/lib/tre-match-parallel.c)).
MinRX normally borrows reusable scratch storage but falls back to private
scratch when another execution already owns it
([`minrx.c`, lines 2026-2087](../ref/minrx/minrx.c)). A new implementation can
make this policy simpler with an explicit caller-independent workspace pool or
thread-local cache; ownership must remain unambiguous.

## Compile once, simplify carefully

### Annotate the syntax tree

Compute, bottom-up, at least:

- nullability;
- possible first and last consuming atoms;
- capture containment and numbering;
- minimum and bounded maximum character length;
- required anchors and a conservative required prefix; and
- the preference of every repetition.

TRE computes nullable, first-position, and last-position sets using an explicit
stack rather than recursive calls
([`lib/tre-compile.c`, lines 1305-1558](../ref/tre/lib/tre-compile.c)). It then
connects a concatenation's last positions to the next first positions and an
iteration's last positions back to its first positions
([`lib/tre-compile.c`, lines 1782-1832](../ref/tre/lib/tre-compile.c)). This
position-automaton construction moves much epsilon-closure work from every
input character to compilation.

The same annotations should also drive safety checks. For example, an
unbounded nullable repetition needs the finite empty-occurrence rule from
Section 8.5; it cannot be treated as an ordinary loop.

### Preserve selection semantics during rewrites

RE2 simplifies counted repetitions and character classes before lowering,
then optimizes and flattens its instruction graph
([`re2/compile.cc`, lines 1115-1197](../ref/re2/re2/compile.cc)). Its flattening
partitions epsilon-connected instructions into lists so repeated traversals do
not expand the program quadratically
([`re2/prog.cc`, lines 528-658](../ref/re2/re2/prog.cc)). These are valuable
ideas, but language equivalence is not enough for POSIX: a rewrite must also
preserve capture participation, last-participation reporting, left-to-right
subpattern priority, and longest/shortest repetition preferences.

Safe early rewrites include merging adjacent character ranges and removing
unreachable nodes. Reassociation, alternation factoring, capture removal, and
duplication expansion need a tagged-equivalence test before use.

### Allocate compilation objects in arenas

AST nodes, temporary position sets, and transition metadata usually share the
compiled expression's lifetime. TRE allocates small objects from blocks and
frees all blocks together
([`lib/tre-mem.c`, lines 9-13 and 90-152](../ref/tre/lib/tre-mem.c)). It also
uses a bounded growable explicit stack
([`lib/tre-stack.c`, lines 66-102](../ref/tre/lib/tre-stack.c)). These techniques
reduce allocator traffic and avoid host call-stack exhaustion on deeply nested
input. Pattern-size and arithmetic limits still need explicit checks before
allocating.

### Normalize character predicates at compilation

Represent a single-character predicate as sorted, non-overlapping ranges plus
any locale operations that cannot be reduced to ranges. MinRX sorts and
coalesces ranges at finalization, then uses binary search for membership
([`charset.c`, lines 8905-8926 and 8943-9014](../ref/minrx/charset.c)). RE2 goes
further for a byte program: it maps bytes that every instruction treats alike
to one equivalence class
([`re2/prog.cc`, lines 452-518](../ref/re2/re2/prog.cc)). The latter can greatly
shrink DFA transition tables.

For this project, build equivalence classes over the locale adapter's semantic
alphabet, not blindly over bytes. Multi-character collating elements consume
sequences, match length is measured in characters, and offsets are bytes. A raw
byte class is safe only after the decoder/lowering layer proves that it cannot
split a character or a collating element. Do not copy MinRX's frozen Unicode
tables or its one-character-only collating-symbol path
([`charset.c`, lines 28-32 and 9237-9249](../ref/minrx/charset.c)); the contract
requires the compilation-time locale.

## Use a bounded parallel matcher as the semantic baseline

Both TRE and MinRX run all viable automaton paths in lockstep and retain at
most one winner per automaton state at a subject boundary. TRE states this
directly and gives linear input-length worst-case behavior for a fixed TNFA
([`lib/tre-match-parallel.c`, lines 9-22](../ref/tre/lib/tre-match-parallel.c)).
Its matcher uses two active-state buffers, a per-state position record to avoid
duplicates, and a tag comparison when two paths meet
([`lib/tre-match-parallel.c`, lines 280-334 and
362-496](../ref/tre/lib/tre-match-parallel.c)).

This is preferable to recursive backtracking as the conformance baseline. With
`S` consuming states, `T` transitions, and `K` ordering/tag values, a simple
implementation uses `O(S*(K+1))` workspace and at most `O(n*T*(K+1))` work
over `n`
subject characters. Record and enforce program, transition, workspace, and
offset limits so arithmetic overflow cannot turn a bounded algorithm into an
unsafe one.

### Make POSIX priority explicit

Whole-match length is only the first discriminator. The state carried by a
path must encode enough information to compare:

1. earliest start;
2. per-repetition shortest or longest choices in nesting order;
3. left-to-right subpattern choices; and
4. the captures belonging to the selected last participation.

TRE inserts tags into the AST, assigns each tag a minimize or maximize
direction, and maps selected tags back to submatch endpoints
([`lib/tre-compile.c`, lines 140-166 and 222-304](../ref/tre/lib/tre-compile.c);
[`lib/tre-internal.h`, lines 201-205 and 227-263](../ref/tre/lib/tre-internal.h)).
At a state merge, its matcher compares the ordered tag vectors rather than
keeping the first path encountered
([`lib/tre-match-parallel.c`, lines 433-491](../ref/tre/lib/tre-match-parallel.c)).

MinRX offers an alternative representation worth prototyping. Its structured
NFA retains entry and exit nodes for alternation, repetition, and groups. Each
path carries a bounded lexical stack; competing paths at the same node compare
the earlier start and then stack entries from outermost to innermost
([`ALGORITHM.txt`, lines 80-159](../ref/minrx/ALGORITHM.txt)). Numbering nodes so
that block interiors precede exits turns the epsilon worklist into a priority
queue that resolves a block before discarding its comparison state
([`ALGORITHM.txt`, lines 107-131](../ref/minrx/ALGORITHM.txt)).

Neither encoding should be accepted on design intuition alone. Differentially
test every merge rule against the examples and boundary ledger in the
specification, especially nested repetitions, empty versus nonparticipating
groups, and captures in the last repetition.

### Isolate minimal-repetition accounting

MinRX brackets shortest-preferring repetitions, accumulates the characters
consumed inside each nesting level, and compares those counters before the
ordinary POSIX priority stack
([`ALGORITHM.txt`, lines 320-420](../ref/minrx/ALGORITHM.txt)). After finding an
accepting path it continues looking for a longer compatible whole match while
rejecting paths that increase an already fixed minimized count
([`ALGORITHM.txt`, lines 424-433](../ref/minrx/ALGORITHM.txt)). This separation
matches the useful design principle in Section 4.3: shortest preference changes
candidate ordering, not the recognized language and not the priorities of
unrelated subpatterns.

Treat the exact counter scheme as a research candidate because of MinRX's
correctness caveat. Tests must include nested minimal repetitions, mixed
minimal and normal repetitions, nullable operands, `REG_MINIMAL` inversion,
and alternatives that permit a longer whole match without lengthening the
minimal repetition.

## Make the hot path sparse and allocation-free

### Sparse state sets and generation marks

RE2's sparse set has constant-time insertion, membership, and clearing while
iteration costs only the number of live entries; insertion order can also act
as a work queue
([`re2/sparse_set.h`, lines 8-30](../ref/re2/re2/sparse_set.h)). TRE obtains a
similar no-clear property by recording the current input position per state.
MinRX uses a hierarchical bitmap whose root word makes clearing constant-time
and whose lowest set member is found with count-trailing-zero operations
([`minrx.c`, lines 373-481](../ref/minrx/minrx.c)).

Use two preallocated sets and swap them after each decoded character. Store
state payloads in arrays indexed by state ID. A generation counter or sparse
dense index avoids clearing every payload slot on each step; handle generation
wrap explicitly.

### Share path metadata until it changes

Capture and priority vectors can dominate copying costs. RE2 identifies capture
copying as its NFA bottleneck
([`re2/onepass.cc`, lines 103-132](../ref/re2/re2/onepass.cc)). MinRX uses
reference-counted copy-on-write vectors plus a freelist: a fork increments the
reference count, and only a write to shared storage clones the vector
([`minrx.c`, lines 158-285](../ref/minrx/minrx.c)).

Possible refinements are persistent tag histories, copy-on-write vectors, or
small inline vectors with pooled overflow. Compare paths using the compact
priority projection first; materialize every capture offset only for the
winner. Any sharing scheme must remain local to one execution or use safe
ownership, because a compiled expression is concurrently executable.

### Specialize `REG_NOSUB`

If `REG_NOSUB` applies, do not allocate, update, or write capture vectors.
MinRX has a separate no-sub executor whose states are only set membership
([`minrx.c`, lines 2569-2748](../ref/minrx/minrx.c)). TRE similarly sets its
runtime tag count to zero when its caller passes no tag buffer
([`lib/tre-match-parallel.c`, lines 155-174](../ref/tre/lib/tre-match-parallel.c)).
A conforming implementation should also select this path whenever `REG_NOSUB`
applies and must leave `pmatch` untouched, a contract TRE itself violates as
documented in the divergence audit.

## Add fast paths behind semantic predicates

Fast paths should be optional executors for the same immutable program. Each
needs a compile-time eligibility predicate and a safe fallback.

### Required-start filtering

TRE derives a unique possible first byte and uses `memchr()` before starting
the TNFA ([`lib/tre-match-parallel.c`, lines
211-236](../ref/tre/lib/tre-match-parallel.c)).
MinRX derives the complete first-character set through epsilon closure, converts
it to possible first bytes, and uses either `memchr()` or a 256-entry lookup
([`minrx.c`, lines 1858-1953 and 2439-2498](../ref/minrx/minrx.c)). RE2 extracts
a required literal prefix and chooses a specialized prefix search
([`re2/regexp.cc`, lines 734-759](../ref/re2/re2/regexp.cc);
[`re2/prog.cc`, lines 1020-1037](../ref/re2/re2/prog.cc)).

Adopt this only when nullability, anchors, `REG_ICASE`, locale classes,
collating elements, and decoding prove the filter has no false negatives.
Stateful multibyte encodings generally require decoding from a known state;
arbitrary byte skipping is then invalid. A UTF-8 candidate byte must be mapped
back to a valid character boundary before matching.

### One-pass execution

RE2 detects programs where, for each state and next byte class, at most one
path can continue. It precomputes the next state, empty-width conditions, and
capture actions in a compact table
([`re2/onepass.cc`, lines 7-51 and 124-168](../ref/re2/re2/onepass.cc)). This
removes active-set maintenance and repeated capture copies.

For POSIX, the eligibility proof must be stronger than RE2's: there must be no
two successful derivations whose POSIX ordering or capture results differ,
including ambiguity caused by collating elements and nullable repetitions. If
that proof fails, use the baseline matcher.

### Lazy DFA for existence and whole-match bounds

RE2 constructs DFA states on demand, caches transitions, reduces each state's
fanout with byte equivalence classes, and stops on a dead state
([`re2/dfa.cc`, lines 1280-1313](../ref/re2/re2/dfa.cc)). Its cache has a hard
memory budget and returns failure rather than allocating beyond it
([`re2/dfa.cc`, lines 743-799](../ref/re2/re2/dfa.cc)). The caller can then fall
back to another executor; RE2 routes among DFA, one-pass, bit-state, and NFA
engines this way ([`re2/re2.cc`, lines 730-904](../ref/re2/re2/re2.cc)).

A POSIX implementation can use an untagged DFA to reject nonmatches or locate
the earliest start and longest end, then run the tagged matcher only over that
candidate interval. It cannot use an untagged DFA to choose captures. Cache
eviction, failed allocation, or an unavailable reverse program must affect
only speed, never the result.

RE2's bit-state executor illustrates another bounded small-input technique: a
bitmap marks each `(instruction list, input position)` pair, preventing repeated
work
([`re2/bitstate.cc`, lines 7-18 and 88-102](../ref/re2/re2/bitstate.cc)). Its
backtracking order is not a POSIX capture algorithm. Reuse the visited-pair idea
only after proving that one visit is sufficient for the POSIX payload, or after
making an improved payload re-enter the worklist.

## Recommended executor order

Use a portfolio rather than forcing all patterns through the most elaborate
engine:

1. Decode and compile against an immutable locale snapshot.
2. Apply required-start filtering when its proof predicate succeeds.
3. Use a POSIX-proven one-pass executor for unambiguous eligible programs.
4. For `REG_NOSUB`, use a capture-free NFA or bounded lazy DFA.
5. For captures, optionally use a DFA to narrow the interval, then run the
   tagged or structured baseline matcher anchored to that interval.
6. On any fast-path resource failure, fall back without changing semantics.

TRE demonstrates the basic feature dispatch by reserving backtracking for
non-regular back-reference extensions and using the parallel matcher for exact
regular expressions
([`lib/regexec.c`, lines 138-199](../ref/tre/lib/regexec.c)). POSIX ERE has no
back-references, so the conforming core does not need an exponential executor.

## Validation and measurement gates

Before enabling an optimization, compare it with the baseline on generated and
adversarial patterns. Compare the complete result: success or error, whole
match, every requested capture, untouched `pmatch` cases, and byte offsets.
Generate subjects by character boundaries and include locale-specific
multi-character collating elements.

Measure at least compile time, program size, peak per-call workspace, states and
transitions visited per character, tag-vector copies, and fallback counts. Use
separate corpora for literal-heavy search, ambiguous captures, large bracket
sets, nullable repetition, and minimal repetition. An optimization is ready
only after differential agreement and a demonstrated resource improvement;
source-level resemblance to a reference is not evidence of POSIX equivalence.

## Verification record

The revisions above were confirmed with `git -C ref/<name> rev-parse HEAD`.
Every cited source path exists in this checkout. This study was static: it did
not modify or build the reference trees, and it makes no upstream test-suite or
benchmark claim.
