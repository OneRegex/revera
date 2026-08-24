# Mistakes log

## 2026-08-24, ERE specification review

- I wrote that `{CHARCLASS_NAME_MAX}` is runtime-increasable "via
  `sysconf()`". The limits are runtime-increasable, but Issue 8 defines a
  `sysconf()` variable only for `{RE_DUP_MAX}` (`_SC_RE_DUP_MAX`). I
  generalized from the `<limits.h>` section preamble without checking the
  `sysconf()` variable table. Lesson: verify each named API hook, not the
  category it sits in.
- I recommended that the specification document `regexec()` errors such as
  `REG_ESPACE` as standard-allowed. The normative RETURN VALUE text only
  specifies `REG_NOMATCH`; the "or an error" wording sits in the
  DESCRIPTION. The right advice was to record the tension, not to add
  outcomes. Lesson: separate the standard's letter from common practice in
  recommendations, exactly as I demanded of the reviewed document.
- I called the `REG_ICASE` `[^a]` behavior "universal practice" after
  testing only macOS libc and macOS grep. Lesson: claim only what was
  tested, and record the probe.

## Go engine (go0/)

- Pooled workspace generation stamps restarted at 1 on every call, so a
  reused workspace treated stale entries as live threads. Symptom: the
  second Exec on the same Regexp failed. Generations now grow across
  calls from a per-workspace base.
- Closures created inside the scan loop allocated on every boundary.
  The executor is now a state struct with methods.
- A test expectation for (a|ab)(c|bcd)(d*) came from memory of other
  engines, not from the specification text. The spec rule (4.3 item 3)
  and macOS libc both give (0,2)(2,3)(3,4).
- The bracket parser let a character class fall through as a range start
  ([[:alpha:]-z] compiled). Non-endpoint items followed by a range
  hyphen are now ERange.
- 1<<31 as an untyped int constant overflowed on GOARCH=386; the length
  guard now compares int64 values.
- A swival review found a panic: the capture solver passed spans longer
  than eight scalars into the equivalence-class matcher, whose candidate
  buffer holds eight. The fast path had the bound; the solver did not.
  matchesMulti now rejects any span above the locale element limit.
- The same review found a conformance gap: a 20-byte pattern with nested
  {255} intervals failed to compile because interval expansion hit the
  fixed program cap. POSIX guarantees capacity for 256-byte patterns.
  Compilation now succeeds without a program; execution answers no-match
  from the minimum match length, and reports ESpace only when the
  subject is long enough to need the oversized program.
- The 64-slot counter mask was also a capacity bug: 65 shortest-
  preferring repetitions fit in 195 bytes of pattern. Slots past 64 now
  ride in an overflow list per instruction.
- I first read section 8.5 as banning every null occurrence past the
  minimum. That made (a?)* on an empty subject report a nonparticipating
  group. The rule's "only available match" exception and the section 4.3
  null-over-nonparticipation preference require one null occurrence when
  the operand can match null. A 4,000-case differential against macOS
  libc caught it; both engines and the tests now implement the fixed
  reading.
- The minimum-length fallback counted every bracket as one character.
  A bracket holding only multi-character collating symbols consumes at
  least the shortest symbol length, so the fallback reported ESpace for
  subjects it could have refused outright. Found by the second review.
- Overflow counter slots were narrowed to uint16, so shortest-repetition
  slot 65,536 wrapped to slot zero and corrupted selection priority in
  patterns with more than 65,535 minimal repetitions. Widened to uint32.
  Found by the second review.
- The capture solver's memo key held the raw instance count, so an
  unbounded repetition created a state per count and a cubic blowup.
  ((a|aa)*) with captures failed at 600 characters. For an unbounded
  maximum the behavior depends on the count only through the minimum,
  so the key now folds larger counts onto it. Split ranges are also
  clamped by per-node length bounds. Found by the third review.
- The whole-program fallback made one oversized branch poison sibling
  branches: (x|huge) on "x" reported ESpace. Oversized subtrees now
  prune to a dead-end instruction, and the program stays exact for any
  subject shorter than the pruned subtree's minimum match length.
- satAdd summed two possibly saturated int values before clamping. On a
  32-bit platform the sum can overflow and corrupt the length bounds
  that the capture solver trusts. The arithmetic now runs in int64.
  Found by the fourth review.
- Replacing the hand-rolled ctrLess loop with generic slices.Compare
  looked like pure cleanup, but it pushed prune and store past the
  inliner budget and slowed the ambiguous-star benchmark by 25
  percent. Reverted with a comment explaining why the loop stays.
  Lesson: in a hot path, benchmark a "trivial" stdlib substitution
  before keeping it.
