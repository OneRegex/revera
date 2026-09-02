# Implementation techniques from the reference engines

Status: source study performed 2026-08-24.

## Scope and baselines

This document extracts implementation techniques that can help implement [`POSIX-1-2024-ERE-SPECIFICATION.md`](POSIX-1-2024-ERE-SPECIFICATION.md).
It is not a claim that any reference implements that contract in full.
The inspected revisions are:

- TRE `71bfcaf0af3994384987c6c2679ed7d078ffe189`.
- RE2 `972a15cedd008d846f1a39b2e88ce48d7f166cbd`.
- MinRX `d13610cdf983337d32b5e07a46da69e40ec5adb0`.

| Reference | Most useful ideas                     | Semantic boundary                  |
| --------- | ------------------------------------- | ---------------------------------- |
| TRE       | Tagged TNFA, arenas, start filtering  | Known Issue 8 gaps                 |
| RE2       | Sparse sets and executor fast paths   | Different syntax, locale, captures |
| MinRX     | Structured NFA and compact path state | Unproven; collation gaps           |

The central rule is to optimize a proved-equivalent representation, not to adopt a reference engine's observable choices.
RE2 uses backtracking-style submatch priority in its NFA on purpose, at [`re2/nfa.cc`, lines 7-22](../third_party/re2/re2/nfa.cc).
MinRX says that its structured-NFA algorithm is not yet proven correct, at [`ALGORITHM.txt`, lines 435-451](../third_party/minrx/ALGORITHM.txt).
TRE's known Issue 8 gaps are recorded separately in [`TRE-POSIX-ERE-DIVERGENCES.md`](TRE-POSIX-ERE-DIVERGENCES.md).

## Separate the immutable program from execution state

Compile the pattern and compilation-time locale into an immutable program.
Keep active states, tag vectors, decoder state, and temporary queues in a per-`regexec()` workspace.
This gives concurrent calls no shared mutable match state, as required by Section 13.3 of the specification.

The compiled object of TRE holds transitions, tags, submatch instructions, first characters and flags.
Its parallel matcher allocates a private workspace for each call.
See [`lib/tre-internal.h`, lines 241-263](../third_party/tre/lib/tre-internal.h) and [`lib/tre-match-parallel.c`, lines 155-205](../third_party/tre/lib/tre-match-parallel.c).
MinRX usually borrows reusable scratch storage.
It falls back to private scratch when another execution already owns that storage, at [`minrx.c`, lines 2026-2087](../third_party/minrx/minrx.c).
A new implementation can make this policy simpler with an explicit workspace pool or a thread-local cache.
Ownership must stay unambiguous.

## Compile once, simplify carefully

### Annotate the syntax tree

Compute, bottom-up, at least:

- nullability.
- possible first and last consuming atoms.
- capture containment and numbering.
- minimum and bounded maximum character length.
- required anchors and a conservative required prefix.
- the preference of every repetition.

TRE computes nullable, first-position, and last-position sets using an explicit stack rather than recursive calls ([`lib/tre-compile.c`, lines 1305-1558](../third_party/tre/lib/tre-compile.c)).
It then connects the last positions of a concatenation to the next first positions.
It connects the last positions of an iteration back to its own first positions.
See [`lib/tre-compile.c`, lines 1782-1832](../third_party/tre/lib/tre-compile.c).
This position-automaton construction moves much epsilon-closure work from every input character to compilation.

The same annotations should also drive safety checks.
For example, an unbounded nullable repetition needs the finite empty-occurrence rule of Section 8.5.
An ordinary loop is not enough.

### Preserve selection semantics during rewrites

RE2 simplifies counted repetitions and character classes before lowering, then optimizes and flattens its instruction graph ([`re2/compile.cc`, lines 1115-1197](../third_party/re2/re2/compile.cc)).
Its flattening partitions epsilon-connected instructions into lists so repeated traversals do not expand the program quadratically ([`re2/prog.cc`, lines 528-658](../third_party/re2/re2/prog.cc)).
These are valuable ideas, but language equivalence is not enough for POSIX.
A rewrite must also keep capture participation, last-participation reporting, left-to-right subpattern priority, and the longest and shortest repetition preferences.

Safe early rewrites include merging adjacent character ranges and removing unreachable nodes.
Reassociation, alternation factoring, capture removal, and duplication expansion need a tagged-equivalence test before use.

### Allocate compilation objects in arenas

AST nodes, temporary position sets, and transition metadata usually share the compiled expression's lifetime.
TRE allocates small objects from blocks and frees all blocks together ([`lib/tre-mem.c`, lines 9-13 and 90-152](../third_party/tre/lib/tre-mem.c)).
It also uses a bounded growable explicit stack ([`lib/tre-stack.c`, lines 66-102](../third_party/tre/lib/tre-stack.c)).
These techniques reduce allocator traffic and avoid host call-stack exhaustion on deeply nested input.
Pattern-size and arithmetic limits still need explicit checks before allocating.

### Normalize character predicates at compilation

Represent a single-character predicate as sorted, non-overlapping ranges plus any locale operations that cannot be reduced to ranges.
MinRX sorts and coalesces ranges at finalization, then uses binary search for membership ([`charset.c`, lines 8905-8926 and 8943-9014](../third_party/minrx/charset.c)).
RE2 goes further for a byte program.
It maps bytes that every instruction treats alike to one equivalence class, at [`re2/prog.cc`, lines 452-518](../third_party/re2/re2/prog.cc).
The latter can greatly shrink DFA transition tables.

For this project, build equivalence classes over the locale adapter's semantic alphabet, not blindly over bytes.
Multi-character collating elements consume sequences, match length is measured in characters, and offsets are bytes.
A raw byte class is safe only after the decoder proves that it cannot split a character or a collating element.
Do not copy the frozen Unicode tables of MinRX, or its one-character-only collating-symbol path.
See [`charset.c`, lines 28-32 and 9237-9249](../third_party/minrx/charset.c).
The contract requires the compilation-time locale.

## Use a bounded parallel matcher as the semantic baseline

TRE and MinRX both run all viable automaton paths in lockstep.
Each keeps at most one winner per automaton state at a subject boundary.
TRE states this directly and gives linear input-length worst-case behavior for a fixed TNFA ([`lib/tre-match-parallel.c`, lines 9-22](../third_party/tre/lib/tre-match-parallel.c)).
Its matcher uses two active-state buffers, a per-state position record against duplicates, and a tag comparison when two paths meet.
See [`lib/tre-match-parallel.c`, lines 280-334 and 362-496](../third_party/tre/lib/tre-match-parallel.c).

This is preferable to recursive backtracking as the conformance baseline.
Take `S` consuming states, `T` transitions, and `K` ordering or tag values.
A simple implementation then uses `O(S*(K+1))` workspace, and at most `O(n*T*(K+1))` work over `n` subject characters.
Record and enforce the program, transition, workspace and offset limits.
Arithmetic overflow then cannot turn a bounded algorithm into an unsafe one.

### Make POSIX priority explicit

Whole-match length is only the first discriminator.
The state carried by a path must encode enough information to compare:

1. the earliest start.
2. the per-repetition shortest or longest choices, in nesting order.
3. the left-to-right subpattern choices.
4. the captures of the selected last participation.

TRE inserts tags into the AST and gives each tag a minimize or maximize direction.
It maps the selected tags back to submatch endpoints.
See [`lib/tre-compile.c`, lines 140-166 and 222-304](../third_party/tre/lib/tre-compile.c) and [`lib/tre-internal.h`, lines 201-205 and 227-263](../third_party/tre/lib/tre-internal.h).
At a state merge, its matcher compares the ordered tag vectors.
It does not simply keep the first path it met, at [`lib/tre-match-parallel.c`, lines 433-491](../third_party/tre/lib/tre-match-parallel.c).

MinRX offers an alternative representation worth prototyping.
Its structured NFA retains entry and exit nodes for alternation, repetition, and groups.
Each path carries a bounded lexical stack.
Competing paths at the same node compare the earlier start first, then the stack entries from outermost to innermost.
See [`ALGORITHM.txt`, lines 80-159](../third_party/minrx/ALGORITHM.txt).
Node numbering that puts block interiors before exits turns the epsilon worklist into a priority queue.
That queue resolves a block before it discards the comparison state of the block.
See [`ALGORITHM.txt`, lines 107-131](../third_party/minrx/ALGORITHM.txt).

Neither encoding should be accepted on design intuition alone.
Test every merge rule differentially, against the examples and the boundary ledger of the specification.
Give most attention to nested repetitions, empty against nonparticipating groups, and captures in the last repetition.

### Isolate minimal-repetition accounting

MinRX brackets the shortest-preferring repetitions and accumulates the characters consumed inside each nesting level.
It compares those counters before the ordinary POSIX priority stack, at [`ALGORITHM.txt`, lines 320-420](../third_party/minrx/ALGORITHM.txt).
After it finds an accepting path, it keeps looking for a longer compatible whole match.
It rejects any path that increases an already fixed minimized count, at [`ALGORITHM.txt`, lines 424-433](../third_party/minrx/ALGORITHM.txt).
This separation matches the design principle of Section 4.3.
Shortest preference changes the candidate ordering.
It changes neither the recognized language nor the priorities of unrelated subpatterns.

Treat the exact counter scheme as a research candidate because of MinRX's correctness caveat.
Tests must cover nested minimal repetitions, mixed minimal and normal repetitions, nullable operands, and `REG_MINIMAL` inversion.
They must also cover alternatives that allow a longer whole match without a longer minimal repetition.

## Make the hot path sparse and allocation-free

### Sparse state sets and generation marks

The sparse set of RE2 has constant-time insertion, membership and clearing, and iteration costs only the number of live entries.
Insertion order can also act as a work queue, at [`re2/sparse_set.h`, lines 8-30](../third_party/re2/re2/sparse_set.h).
TRE obtains a similar no-clear property by recording the current input position per state.
MinRX uses a hierarchical bitmap.
Its root word makes clearing constant-time, and a count-trailing-zero operation finds the lowest set member.
See [`minrx.c`, lines 373-481](../third_party/minrx/minrx.c).

Use two preallocated sets and swap them after each decoded character.
Store state payloads in arrays indexed by state ID.
A generation counter or a sparse dense index avoids a clear of every payload slot on each step.
Handle the generation wrap explicitly.

### Share path metadata until it changes

Capture and priority vectors can dominate copying costs.
RE2 identifies capture copying as its NFA bottleneck ([`re2/onepass.cc`, lines 103-132](../third_party/re2/re2/onepass.cc)).
MinRX uses reference-counted copy-on-write vectors and a freelist.
A fork increments the reference count, and only a write to shared storage clones the vector.
See [`minrx.c`, lines 158-285](../third_party/minrx/minrx.c).

Possible refinements are persistent tag histories, copy-on-write vectors, or small inline vectors with pooled overflow.
Compare paths with the compact priority projection first.
Build every capture offset for the winner only.
Any sharing scheme must remain local to one execution or use safe ownership, because a compiled expression is concurrently executable.

### Specialize `REG_NOSUB`

If `REG_NOSUB` applies, do not allocate, update, or write capture vectors.
MinRX has a separate no-sub executor whose states are only set membership ([`minrx.c`, lines 2569-2748](../third_party/minrx/minrx.c)).
TRE similarly sets its runtime tag count to zero when its caller passes no tag buffer ([`lib/tre-match-parallel.c`, lines 155-174](../third_party/tre/lib/tre-match-parallel.c)).
A conforming implementation should select this path whenever `REG_NOSUB` applies.
It must leave `pmatch` untouched, and the divergence audit records that TRE itself breaks that contract.

## Add fast paths behind semantic predicates

Fast paths should be optional executors for the same immutable program.
Each needs a compile-time eligibility predicate and a safe fallback.

### Required-start filtering

TRE derives a unique possible first byte and uses `memchr()` before starting the TNFA ([`lib/tre-match-parallel.c`, lines 211-236](../third_party/tre/lib/tre-match-parallel.c)).
MinRX derives the complete first-character set through the epsilon closure, and converts it to possible first bytes.
It then uses either `memchr()` or a 256-entry lookup.
See [`minrx.c`, lines 1858-1953 and 2439-2498](../third_party/minrx/minrx.c).
RE2 extracts a required literal prefix and chooses a specialized prefix search.
See [`re2/regexp.cc`, lines 734-759](../third_party/re2/re2/regexp.cc) and [`re2/prog.cc`, lines 1020-1037](../third_party/re2/re2/prog.cc).

Adopt this only when nullability, anchors, `REG_ICASE`, locale classes, collating elements, and decoding prove the filter has no false negatives.
A stateful multibyte encoding usually needs decoding from a known state.
Arbitrary byte skipping is then invalid.
A UTF-8 candidate byte must be mapped back to a valid character boundary before matching.

### One-pass execution

RE2 detects programs where, for each state and next byte class, at most one path can continue.
It precomputes the next state, empty-width conditions, and capture actions in a compact table ([`re2/onepass.cc`, lines 7-51 and 124-168](../third_party/re2/re2/onepass.cc)).
This removes active-set maintenance and repeated capture copies.

For POSIX, the eligibility proof must be stronger than the one RE2 uses.
No two successful derivations may differ in their POSIX ordering or capture results.
That includes the ambiguity that collating elements and nullable repetitions cause.
If that proof fails, use the baseline matcher.

### Lazy DFA for existence and whole-match bounds

RE2 builds DFA states on demand and caches the transitions.
Byte equivalence classes reduce the fanout of each state, and the run stops on a dead state.
See [`re2/dfa.cc`, lines 1280-1313](../third_party/re2/re2/dfa.cc).
Its cache has a hard memory budget and returns failure rather than allocating beyond it ([`re2/dfa.cc`, lines 743-799](../third_party/re2/re2/dfa.cc)).
The caller can then fall back to another executor.
RE2 routes among its DFA, one-pass, bit-state and NFA engines this way, at [`re2/re2.cc`, lines 730-904](../third_party/re2/re2/re2.cc).

A POSIX implementation can use an untagged DFA to reject nonmatches, or to locate the earliest start and the longest end.
It then runs the tagged matcher over that candidate interval only.
It cannot use an untagged DFA to choose captures.
Cache eviction, failed allocation, or an unavailable reverse program must affect only speed, never the result.

The bit-state executor of RE2 shows another bounded small-input technique.
A bitmap marks each `(instruction list, input position)` pair, which prevents repeated work.
See [`re2/bitstate.cc`, lines 7-18 and 88-102](../third_party/re2/re2/bitstate.cc).
Its backtracking order is not a POSIX capture algorithm.
Reuse the visited-pair idea only after a proof that one visit is enough for the POSIX payload.
An improved payload must otherwise re-enter the worklist.

## Recommended executor order

Use a portfolio rather than forcing all patterns through the most elaborate engine:

1. Decode and compile against an immutable locale snapshot.
2. Apply required-start filtering when its proof predicate succeeds.
3. Use a POSIX-proven one-pass executor for unambiguous eligible programs.
4. For `REG_NOSUB`, use a capture-free NFA or bounded lazy DFA.
5. For captures, a DFA can narrow the interval first.
   Then run the tagged or structured baseline matcher, anchored to that interval.
6. On any fast-path resource failure, fall back without changing semantics.

TRE shows the basic feature dispatch.
It reserves backtracking for the non-regular back-reference extensions, and uses the parallel matcher for exact regular expressions.
See [`lib/regexec.c`, lines 138-199](../third_party/tre/lib/regexec.c).
POSIX ERE has no back-references, so the conforming core does not need an exponential executor.

## Validation and measurement gates

Before enabling an optimization, compare it with the baseline on generated and adversarial patterns.
Compare the complete result: success or error, whole match, every requested capture, untouched `pmatch` cases, and byte offsets.
Generate subjects by character boundaries and include locale-specific multi-character collating elements.

Measure at least the compile time, the program size, and the peak per-call workspace.
Measure also the states and transitions visited per character, the tag-vector copies, and the fallback counts.
Use separate corpora for literal-heavy search, ambiguous captures, large bracket sets, nullable repetition, and minimal repetition.
An optimization is ready only after differential agreement and a measured resource improvement.
A resemblance to reference source code is not evidence of POSIX equivalence.

## Verification record

The revisions above were confirmed with `git -C third_party/<name> rev-parse HEAD`.
Every cited source path exists in this checkout.
This study was static.
It did not modify or build the reference trees.
It makes no claim about an upstream test suite or benchmark.
