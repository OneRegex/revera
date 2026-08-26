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
- The first queue-dedupe attempt for the resource contract put the
  membership check inside relax. That pushed relax past the inliner
  budget (cost 90 over budget 80) and cost 12 percent on the
  ambiguous-star benchmark. The check moved to a cold compaction pass
  that runs only when the queue passes twice the program length, and
  the hot append stayed inlined. Lesson: the engine comments say which
  functions are inline-tuned; verify with -gcflags=-m after any edit
  near them.
- A contract test assumed nested intervals like ((((a{200}){200})...)
  leave a nil program. They do not: emitRepeat prunes each oversized
  subtree to a dead end, and the program stays real with a failMin.
  Only instructions that cannot be pruned, such as a pattern with over
  a million literal characters, pass the cap and leave prog nil. The
  test now compiles such a literal.

- 2026-08-24, replace_test.go: I added a strings.ToUpper call in a new
  test without adding the strings import, so the build failed once.
  I fixed the import list right after.

- 2026-08-24, go1/revera/capture.go: my first translation of the
  capture solver kept unbounded arenas with int32 offsets. On
  ((a*){250}){250} over 300 characters the kid arena passed 2^31
  entries and the offsets went negative. go0 survives the same input
  only because it burns 20 GB before the work limit reports ESpace.
  I added solverArenaLimit so go1 reports ESpace early and cleanly.

- 2026-08-24, go1/revera: my first Vego translation broke four of my
  own subset rules: a break whose target was a switch, a function
  with three results, a const declared inside a function, and a
  uint conversion. My own vego2json checker caught all of them, which
  is the point of having it, but I should have followed the
  specification exactly while writing the code.

- 2026-08-24, go1 review pass: swival found real gaps I had left. The
  locale loader validated section bounds but not the offsets stored
  inside sections, so a malformed blob could panic instead of being
  rejected. The subset checker also under-enforced its own spec:
  pointer locals and results, writable-slice escapes of globals,
  rune-semantics string conversions, a crash on malformed range
  statements, and zero-argument append all slipped through. I fixed
  the code, tightened the checker, aligned the spec, and added tests
  for every case.

## 2026-08-25, target printers (Zig/C++/Rust)

- The first Zig driver used one arena per compiled pattern and
  reset it only at the next compile. Exec workspaces for the
  solver-heavy corpus patterns piled up gigabytes across the 88
  Exec commands sharing one pattern, and the process thrashed. Go
  hid this cost behind the garbage collector. Lesson: when removing
  a GC, map every allocation to an explicit lifetime first; the
  workspace lifetime (one Exec) was shorter than the arena I gave
  it (one pattern).
- I diagnosed the hang at the wrong corpus line because the driver
  buffered its output; the stall point I computed from output line
  count was off by the buffered tail. Per-line flushes made the
  next diagnosis exact. Lesson: make the failure observable before
  locating it.
- I first wrote the checker to type constants strictly in
  declaration order; the engine references constants ahead of
  their declaration, which Go allows. On-demand resolution fixed
  it. Lesson: Go declaration-order freedom applies to constants
  too, not only functions.
- I piped the first swival review through `tail -80`, which threw
  away the top-priority findings; only items 8 through 13
  survived. Lesson: capture full review output to a file, then
  read it.
- The C++ evaluation-order pinning copied pointer-typed arguments
  into temporaries. A Vego `*S` parameter lowers to a C++
  reference, so `auto _t = b;` copied the referent struct, and the
  callee mutated the copy. Symptom: one alternation pattern lost
  its program fragments. Lesson: a lowering that changes an
  argument's binding class (reference to value) is not a pure
  reordering.
- The C++ and Zig runtimes left the spare capacity of grown
  buffers uninitialized. Go zeroes allocations, and the engine
  extends slices inside capacity (`s[:off+n]`), reading memory it
  never wrote. The corpora passed for days on zero pages fresh
  from the OS; recycled arena memory and allocator perturbations
  flipped results. Poisoning the spare region made the dependence
  deterministic and provable. Lesson: match the source language's
  allocation contract, not the behavior observed on a lucky
  allocator, and poison what must not be read to prove it is not.
- Two debug bisects chased phantoms because the test loop ignored
  build failures and reused stale binaries, and because the
  underlying nondeterminism made "pass" unrepeatable. Lesson:
  before bisecting, pin down that the failure is deterministic and
  that every probe actually rebuilt.
- My first fix for zero-length array views replaced a null pointer
  with `reinterpret_cast<T*>(alignof(T))` and then did arithmetic
  on it, which is itself undefined behavior. The review caught it;
  a static sentinel object now provides real storage. Lesson: do
  not patch undefined behavior with a smaller undefined behavior.
- The first LEAN4 elaborator dropped the slot counter when leaving a
  nested block, so functions under-reported their frame size and the
  interpreter wrote past the frame. The probe run caught it at the
  first call with block-local variables. Lesson: when a tree walk
  threads a monotone counter, every branch must return it, including
  the branches whose bindings go out of scope.
- Two shell one-liners died on `zsh` because a bare `=====` separator
  token triggers `=word` expansion. Lesson: quote decorative
  separators in zsh commands.
- The LEAN4 driver session forgot the int32 narrowing that the Go,
  Zig, C++ and Rust drivers apply to the O command bounds. The
  crosscheck corpus never used out-of-range bounds, so 86689
  agreeing commands hid the gap; the swival review found it with a
  reproducer. Fixed in the session, and the corpus now carries two
  out-of-range O commands so every driver proves the narrowing.
  Lesson: a protocol harness needs corpus lines at the numeric
  edges of every parsed field, not only realistic values.
