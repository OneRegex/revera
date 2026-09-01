# Implementation notes and progress

This file tracks the Go cleanroom implementation of `docs/POSIX-1-2024-ERE-SPECIFICATION.md`.

## Status

- [x] Module `revera` created in `go0/`.
- [x] Locale data generator (`gen/genlocale`) and `locale` package.
  Differential dump against the C code: bit-identical output.
- [x] Parser with POSIX error codes.
  Spec section 16 tests pass.
- [x] Oracle matcher (`oracle.go`), the exhaustive reference.
- [x] Public API: `Compile`, `Exec`, `NumSub`, flags, errors.
- [x] Global operations: `MatchAll`, `ReplaceAll`, `ReplaceAllFunc`.
  They follow the sed iteration rule for null matches and take a match limit, where a negative value means no bound.
- [x] Phase A engine: single-pass parallel NFA, pooled workspace, zero allocations on the match-only path.
- [x] Phase B capture solver: memoized best parse over the fixed span.
- [x] Differential tests: engine vs oracle (about 8,000 random patterns across flag sets), engine vs Go `regexp.CompilePOSIX` for whole match on long subjects, multi-character elements in cs locale.
- [x] Race detector clean.
  It cross-compiles for 386, arm64 and s390x.

- [x] Scan fast path.
  The executor skips ahead to the next possible first byte when no thread is live, and when no bracket, dot or accepting state is reachable from an ordinary boundary.
  A single byte uses `strings.IndexByte`.
  Newline mode adds a stop at every newline for line anchors.
  Stop bytes are ASCII or UTF-8 lead bytes, so a hit is always a valid boundary.
  This is the required-start filtering technique from the study document, with the filter derived from the start closure per anchor context.
  Benchmarks: literal search moved from 82 MB/s to about 10 GB/s, and a no-match scan to about 50 GB/s, with identical differential results.

Current benchmarks (Apple M4, `go test -bench .`):

| Benchmark               | Speed      | Allocations       |
| ----------------------- | ---------- | ----------------- |
| Literal tail search     | ~20 GB/s   | 0 per op          |
| No-match literal scan   | ~60 GB/s   | 0 per op          |
| Ambiguous `(a\|b)*abb`  | ~25 MB/s   | 0 per op          |
| Class-heavy pattern     | ~35 MB/s   | 0 per op          |
| Small-span captures     | ~1.3 us/op | 4 (192 B) per op  |
| One-pass capture walk   | ~30 us     | 0 per op          |
| Same span, forced solver| ~17 ms     | 3,771 (68 MB)     |

Open items:

- [x] One-pass captures.
  Compile-time analysis proves that every span has at most one parse.
  Phase B then reads the groups from one deterministic walk instead of the memoized solver.
  See the engine-architecture section for the eligibility rules.
  Phase A stays parallel: a genuinely ambiguous pattern still keeps every viable thread live at NFA speed, and a lazy DFA remains the future lever for that case.
- [x] Phase B parse trees and child lists come from pooled bump arenas.
  The maps and scratch are pooled too.
  It runs only when the caller asks for group captures, and only over the selected span.
- [x] The libc differential harness is a cgo test now (`libc_differential_test.go`, macOS only, with the C wrapper in `internal/libcre`).
  It replays the same seeded 20,000-case corpus as the old manual run in `tmp/`, classifies the two documented divergence classes, and fails on anything else.
  The current count is exactly the 18 known cases.
- [x] Resource contracts (`contract.go`).
  `CompileWithContract` returns worst-case heap, stack and step bounds per backend, for one Exec call on a subject of at most `maxInput` bytes.
  An application can therefore refuse an expression before it runs.
  Heap counts the explicit allocations with fixed 64-bit sizes.
  Stack multiplies the deepest call chain by a fixed frame estimate.
  Steps count abstract operations.
  The solver bound folds in the work limit, and its per-step cost pays for the candidate tree comparisons the work counter does not see.
  To keep the matcher bound linear, the closure queue now compacts duplicates past twice the program length.
  The hot push stays a bare inlineable append, and the benchmarks stayed within noise.

## Design decisions

### Input model

- The Go API uses length-delimited strings, not NUL-terminated ones.
- NUL is an ordinary character.
  Dot does not match NUL, as section 8.1 says.
- Input must be UTF-8.
  An invalid byte matches no atom and no dot.
  A match can never start or end inside an invalid byte sequence.
- These choices keep the C-representable domain bit-identical to POSIX.
  NUL-containing input is a permitted extension area.

### Locale handling

- The tables come from the same CLDR 48.2 data as the C implementation.
  A generator parses `src/rv_locale_data.inc` and writes `locale/data.bin`.
- The blob is embedded with `go:embed` as a string.
  Lookups read fixed-width little-endian fields in place.
  No table is decoded into heap slices.
- The POSIX locale is computed, not stored, exactly as in `rv_locale.c`.
- Non-POSIX locales reject range expressions.
  POSIX permits this policy.

### Match selection order (section 4.3 reading)

Candidates for one `regexec` call are compared in this order:

1. Earlier start position wins.
2. Shortest-preferring repetition consumed totals, in pattern pre-order.
   Smaller wins.
   A repetition that does not participate counts zero.
3. Longer whole match wins.
4. Structural pre-order walk of the pattern.
   At each subpattern, compare the consumed span per the ordinary longest rule.
   At a shortest-preferring repetition, shorter wins.
   Earlier (outer, then left) subpatterns win first.

Evidence from the spec examples:

- `.*?c` on `abc abc` selects `abc`.
  So rule 2 outranks whole length.
- `(.*?).*` on `abcdef` selects the whole subject with group 1 empty.
  So after minimal counters tie at zero, whole length still applies.
- `(a?)(ab)?` style cases show whole length outranks inner longest choices.

### Node-level comparison details

The candidate order above needs a per-node rule.
Two parses of the same pattern over the same start compare like this:

- The counter vector has one slot per shortest-preferring repetition, in pattern pre-order.
  A slot holds the total characters that repetition consumed, summed over all its participations.
  Nonparticipation is zero.
  Smaller slots win, compared lexicographically.
- After counters and whole length, walk the pattern pre-order.
  At each node, a participating parse beats a nonparticipating one.
  A longer node span beats a shorter one.
  At a shortest-preferring repetition node, a shorter span beats a longer one.
- At a repetition, compare instance spans left to right, then recurse into each instance.
  The first instance with a longer span wins for a longest-preferring repetition.
  The empty-occurrence rule makes equal totals imply equal instance counts.
- Instance distribution inside a shortest-preferring repetition compares shorter-first.
  The spec does not pin this down.
  This is our documented interpretation, applied identically in the oracle and the engine.

### Engine architecture

- Phase A finds the selected start and end.
  It is a parallel NFA run over all starts at once.
  Thread state is (pc, start, counters).
  Merges keep the best (start, counters) pair.
  Counters only grow, and futures from a shared pc are identical, so the merge is sound.
  With no shortest-preferring repetition the payload is just the start.
- `NoSub`, or `nmatch <= 1`, needs nothing else.
- Phase B computes captures over the fixed selected span.
  It is a memoized best-parse search per (node, i, j).
  Sibling segments of the comparison vector are independent, so a per-node best parse is context-free and memoization is valid.
- The test oracle enumerates all parses and applies the comparison directly.
  It validates both phases differentially.

### One-pass captures

The techniques study requires a strong proof before one-pass execution: no two successful derivations may differ in POSIX ordering or captures.
Parse uniqueness implies that directly, so `Compile` proves uniqueness instead of analyzing orderings.
A node is one-pass when:

- it is an atom, an anchor, or a bracket without multi-character elements (a multi-character element makes the consumed length ambiguous).
- it is a concatenation of one-pass children with at most one variable-length child, so every split point follows from the span length by arithmetic.
- it is a repetition whose operand has a fixed nonempty length, which forces the instance count and rules out null occurrences.
- it is an alternation whose branches have pairwise-disjoint length ranges.
  It may instead have exact pairwise-disjoint first sets, with at most one nullable branch.
  The span length or one lookahead character then selects the branch.

Length classifications never trust saturated bounds: two lengths saturated at `lenInf` compare equal without being exact.
When the proof succeeds, phase B walks the selected span once, verifies every step, and fills the groups with zero allocations.
Any inconsistency makes the walk report failure and the solver take over, so a defect in the walk can only cost speed.
The walk-versus-solver comparison runs in the one-pass tests over random eligible patterns, and the ordinary differential tests cover the path against the oracle as well.

Concatenations with several variable-length children stay on the solver even when a separator would make them unambiguous, as in `(a*)-(b*)`.
Follow-set analysis could admit them later.

### Chosen outcomes in undefined or unspecified areas

All undefined pattern spellings are rejected with a specific error code.
These are the unspecified choices.
An ordinary list never matches an implicit multi-character collating element.
A character class has no multi-character part.
A range in a non-POSIX locale is rejected with `ERange`.
An equivalence class as a range endpoint is rejected with `ERange`.
An empty POSIX-locale range is rejected with `ERange`.
A delimiter-shaped ambiguous list, such as `[.a.]` or `[=a=]`, parses as an ordinary matching list, because the inner special openers need a second `[`.
Undefined escapes (backslash before an ordinary character) are rejected (`BadPat`).
A negated list never matches a multi-character element.

### Empty-occurrence rule (section 8.5)

Two cases govern null occurrences:

- When the repetition consumes at least one character, a null occurrence is allowed only to reach the minimum count.
  Once the minimum is met, no further null occurrences are added.
- A repetition that matches the null string, and whose maximum is not zero, takes null occurrences of its operand when the operand has a null match.
  The null match is then its only available match, and section 4.3 prefers a null match over nonparticipation.
  The count is the minimum, or one when the minimum is zero.
  Without a null operand match, the repetition takes zero occurrences and its groups do not participate.

The first draft dropped the second case and reported `(a?)*` on the empty string with a nonparticipating group.
A differential run against macOS libc exposed the misreading.
After the fix, 20,000 random pattern/subject pairs, including anchors and negated classes, agree with libc on the full capture vectors except 18 cases in two classes:

- libc reports nonparticipation for `(x){0,n}`-style intervals in exactly the situation where it reports participation for `(x)*`.
  The spec states one rule for `*`, `?`, and intervals, so this implementation stays uniform.
- libc skips a leftmost empty match in favor of a later non-empty one, as in `((c){0,2}$)?` on `caac`.
  Section 4.3 rule 1 says a shorter earlier match beats a longer later match, so the empty match at offset zero is selected here.

## Observations

- macOS libc agrees with rule 3 of section 4.3 on the classic case: `(a|ab)(c|bcd)(d*)` on `abcd` gives `(0,2)(2,3)(3,4)`.
  Group 1 takes its longest compatible match.
  Folklore expectations of `(0,1)(1,4)` follow a different reading and do not bind this implementation.
- Go's `regexp.CompilePOSIX` diverges from POSIX on newline handling: dot excludes newline, negated classes exclude newline, and anchors match at line boundaries.
  The stdlib differential avoids newline in subjects and negated classes in patterns for this reason.
