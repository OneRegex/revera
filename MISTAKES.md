# Mistakes log

## 2026-08-24, ERE specification review

- I wrote that `{CHARCLASS_NAME_MAX}` is runtime-increasable "via `sysconf()`".
  The limits are runtime-increasable, but Issue 8 defines a `sysconf()` variable only for `{RE_DUP_MAX}` (`_SC_RE_DUP_MAX`).
  I generalized from the `<limits.h>` section preamble without checking the `sysconf()` variable table.
  Lesson: verify each named API hook, not the category it sits in.
- I recommended that the specification document `regexec()` errors such as `REG_ESPACE` as standard-allowed.
  The normative RETURN VALUE text only specifies `REG_NOMATCH`.
  The "or an error" wording sits in the DESCRIPTION.
  The right advice was to record the tension, not to add outcomes.
  Lesson: separate the standard's letter from common practice in recommendations, exactly as I demanded of the reviewed document.
- I called the `REG_ICASE` `[^a]` behavior "universal practice" after testing only macOS libc and macOS grep.
  Lesson: claim only what was tested, and record the probe.

## Go engine (go0/)

- Pooled workspace generation stamps restarted at 1 on every call, so a reused workspace treated stale entries as live threads.
  Symptom: the second Exec on the same Regexp failed.
  Generations now grow across calls from a per-workspace base.
- Closures created inside the scan loop allocated on every boundary.
  The executor is now a state struct with methods.
- A test expectation for (a|ab)(c|bcd)(d*) came from memory of other engines, not from the specification text.
  The spec rule (4.3 item 3) and macOS libc both give (0,2)(2,3)(3,4).
- The bracket parser let a character class fall through as a range start ([[:alpha:]-z] compiled).
  Non-endpoint items followed by a range hyphen are now ERange.
- 1<<31 as an untyped int constant overflowed on GOARCH=386.
  The length guard now compares int64 values.
- A swival review found a panic: the capture solver passed spans longer than eight scalars into the equivalence-class matcher, whose candidate buffer holds eight.
  The fast path had the bound.
  The solver did not.
  matchesMulti now rejects any span above the locale element limit.
- The same review found a conformance gap: a 20-byte pattern with nested {255} intervals failed to compile because interval expansion hit the fixed program cap.
  POSIX guarantees capacity for 256-byte patterns.
  Compilation now succeeds without a program.
  Execution answers no-match from the minimum match length, and reports ESpace only when the subject is long enough to need the oversized program.
- The 64-slot counter mask was also a capacity bug: 65 shortest- preferring repetitions fit in 195 bytes of pattern.
  Slots past 64 now ride in an overflow list per instruction.
- I first read section 8.5 as banning every null occurrence past the minimum.
  That made (a?)* on an empty subject report a nonparticipating group.
  The rule's "only available match" exception and the section 4.3 null-over-nonparticipation preference require one null occurrence when the operand can match null.
  A 4,000-case differential against macOS libc caught it.
  Both engines and the tests now implement the fixed reading.
- The minimum-length fallback counted every bracket as one character.
  A bracket holding only multi-character collating symbols consumes at least the shortest symbol length, so the fallback reported ESpace for subjects it could have refused outright.
  Found by the second review.
- Overflow counter slots were narrowed to uint16, so shortest-repetition slot 65,536 wrapped to slot zero and corrupted selection priority in patterns with more than 65,535 minimal repetitions.
  Widened to uint32.
  Found by the second review.
- The capture solver's memo key held the raw instance count, so an unbounded repetition created a state per count and a cubic blowup.
  ((a|aa)*) with captures failed at 600 characters.
  For an unbounded maximum the behavior depends on the count only through the minimum, so the key now folds larger counts onto it.
  Split ranges are also clamped by per-node length bounds.
  Found by the third review.
- The whole-program fallback made one oversized branch poison sibling branches: (x|huge) on "x" reported ESpace.
  Oversized subtrees now prune to a dead-end instruction, and the program stays exact for any subject shorter than the pruned subtree's minimum match length.
- satAdd summed two possibly saturated int values before clamping.
  On a 32-bit platform the sum can overflow and corrupt the length bounds that the capture solver trusts.
  The arithmetic now runs in int64.
  Found by the fourth review.
- Replacing the hand-rolled ctrLess loop with generic slices.Compare looked like pure cleanup, but it pushed prune and store past the inliner budget and slowed the ambiguous-star benchmark by 25 percent.
  Reverted with a comment explaining why the loop stays.
  Lesson: in a hot path, benchmark a "trivial" stdlib substitution before keeping it.
- The first queue-dedupe attempt for the resource contract put the membership check inside relax.
  That pushed relax past the inliner budget (cost 90 over budget 80) and cost 12 percent on the ambiguous-star benchmark.
  The check moved to a cold compaction pass that runs only when the queue passes twice the program length, and the hot append stayed inlined.
  Lesson: the engine comments say which functions are inline-tuned.
  Verify with -gcflags=-m after any edit near them.
- `lenInf` served as both the "nothing was pruned" sentinel and a saturated length bound.
  A pruned subtree whose minimum length saturates at `lenInf` therefore looked like no pruning at all.
  The sentinel is now the distinct `failMinNone`.
  Found by a later review.
- The `failMin` gate was more conservative than it had to be.
  Past that bound, an existence-only call now runs the pruned program anyway, because pruning removes possibilities and adds none.
  A match it still finds is therefore a genuine match.
  A miss keeps reporting `ESpace`, and so does a request for offsets, because the pruned program could select the wrong spans.
- The minimum-length fallback gave every equivalence class one character, because its membership looked unenumerable.
  Every element that shares a primary weight with another appears in the equivalence pair sections, so `locale.MinEquivLength` scans them for the shortest member.
  Czech `[[=ch=]]` now counts two characters, and Danish `[[=aa=]]` counts one, through `å`.
- A contract test assumed nested intervals like ((((a{200}){200})...) leave a nil program.
  They do not: emitRepeat prunes each oversized subtree to a dead end, and the program stays real with a failMin.
  Only instructions that cannot be pruned, such as a pattern with over a million literal characters, pass the cap and leave prog nil.
  The test now compiles such a literal.

- 2026-08-24, replace_test.go: I added a strings.ToUpper call in a new test without adding the strings import, so the build failed once.
  I fixed the import list right after.

- 2026-08-24, go1/revera/capture.go: my first translation of the capture solver kept unbounded arenas with int32 offsets.
  On ((a*){250}){250} over 300 characters, the kid arena passed 2^31 entries and the offsets went negative.
  go0 survives the same input only because it burns 20 GB before the work limit reports ESpace.
  I added solverArenaLimit so go1 reports ESpace early and cleanly.

- 2026-08-24, go1/revera: my first Vego translation broke four of my own subset rules.
  Those were a break whose target was a switch, a function with three results, a const declared inside a function, and a uint conversion.
  My own vego2json checker caught all of them, which is the point of having it, but I should have followed the specification exactly while writing the code.

- 2026-08-24, go1 review pass: swival found real gaps I had left.
  The locale loader validated section bounds but not the offsets stored inside sections, so a malformed blob could panic instead of being rejected.
  The subset checker also under-enforced its own spec: pointer locals and results, writable-slice escapes of globals, rune-semantics string conversions, a crash on malformed range statements, and zero-argument append all slipped through.
  I fixed the code, tightened the checker, aligned the spec, and added tests for every case.

## 2026-08-25, target printers (Zig/C++/Rust)

- The first Zig driver used one arena per compiled pattern and reset it only at the next compile.
  Exec workspaces for the solver-heavy corpus patterns piled up gigabytes across the 88 Exec commands sharing one pattern, and the process thrashed.
  Go hid this cost behind the garbage collector.
  Lesson: when removing a GC, map every allocation to an explicit lifetime first.
  The workspace lifetime (one Exec) was shorter than the arena I gave it (one pattern).
- I diagnosed the hang at the wrong corpus line because the driver buffered its output.
  The stall point I computed from output line count was off by the buffered tail.
  Per-line flushes made the next diagnosis exact.
  Lesson: make the failure observable before locating it.
- I first wrote the checker to type constants strictly in declaration order.
  The engine references constants ahead of their declaration, which Go allows.
  On-demand resolution fixed it.
  Lesson: Go declaration-order freedom applies to constants too, not only functions.
- I piped the first swival review through `tail -80`, which threw away the top-priority findings.
  Only items 8 through 13 survived.
  Lesson: capture full review output to a file, then read it.
- The C++ evaluation-order pinning copied pointer-typed arguments into temporaries.
  A Vego `*S` parameter lowers to a C++ reference, so `auto _t = b;` copied the referent struct, and the callee mutated the copy.
  Symptom: one alternation pattern lost its program fragments.
  Lesson: a lowering that changes an argument's binding class (reference to value) is not a pure reordering.
- The C++ and Zig runtimes left the spare capacity of grown buffers uninitialized.
  Go zeroes allocations, and the engine extends slices inside capacity (`s[:off+n]`), reading memory it never wrote.
  The corpora passed for days on zero pages fresh from the OS.
  Recycled arena memory and allocator perturbations flipped results.
  Poisoning the spare region made the dependence deterministic and provable.
  Lesson: match the source language's allocation contract, not the behavior observed on a lucky allocator, and poison what must not be read to prove it is not.
- Two debug bisects chased phantoms because the test loop ignored build failures and reused stale binaries, and because the underlying nondeterminism made "pass" unrepeatable.
  Lesson: before bisecting, pin down that the failure is deterministic and that every probe actually rebuilt.
- My first fix for zero-length array views replaced a null pointer with `reinterpret_cast<T*>(alignof(T))` and then did arithmetic on it, which is itself undefined behavior.
  The review caught it.
  A static sentinel object now provides real storage.
  Lesson: do not patch undefined behavior with a smaller undefined behavior.
- The first LEAN4 elaborator dropped the slot counter when leaving a nested block, so functions under-reported their frame size and the interpreter wrote past the frame.
  The probe run caught it at the first call with block-local variables.
  Lesson: when a tree walk threads a monotone counter, every branch must return it, including the branches whose bindings go out of scope.
- Two shell one-liners died on `zsh` because a bare `=====` separator token triggers `=word` expansion.
  Lesson: quote decorative separators in zsh commands.
- The LEAN4 driver session forgot the int32 narrowing that the Go, Zig, C++ and Rust drivers apply to the O command bounds.
  The crosscheck corpus never used out-of-range bounds, so 86689 agreeing commands hid the gap.
  The swival review found it with a reproducer.
  Fixed in the session, and the corpus now carries two out-of-range O commands so every driver proves the narrowing.
  Lesson: a protocol harness needs corpus lines at the numeric edges of every parsed field, not only realistic values.
- The first metered step check in the LEAN4 driver counted every executed statement against ContractSteps.
  A one-byte subject pays Exec's fixed setup, and three corpus commands passed the figure by up to 1.2x.
  The contract counts abstract operations, so the meter now counts loop iterations and calls, which is the granularity contract.go describes.
  Statements stay a calibration counter.
  Lesson: pick the measurement unit from the bound's own definition, then calibrate against real runs before enforcing.
- The meter-soundness proofs first assumed every do-block is a right-nested bind chain.
  The do elaborator instead inlines the continuation into match arms, emits join points after mid-block statements, and compiles mutable frame variables into projections.
  Each wrong guess showed up as a failed split or apply.
  The fix was structural: case on the scrutinee before touching the binds, re-run the bind simp after every split, and restructure two interpreter helpers into tail form.
  Lesson: read the elaborated term in the error before scripting tactics against imagined shapes.

## 2026-08-27, public APIs of the four targets

- My first Go wrapper filled the offset methods from a `pmatch` slice that `Exec` refuses to write when the expression was compiled with `FlagNoSub`.
  `Exec` reports success and leaves the slice as it is, so `FindStringIndex` returned the zero value `0, 0`, a silent wrong answer.
  The wrapper now checks the flag itself and reports `ErrENoSub`.
  Lesson: when a low-level call answers a narrower question than the wrapper's signature promises, the wrapper has to close the gap, not assume it.
- I gave the C++ `Options` struct default member initializers and nested it inside `Regex`.
  A nested class's default member initializers are parsed only after the enclosing class is complete, so `const Options& options = {}` in a `Regex` member declaration does not compile.
  Moving `Options` to namespace scope fixed it.
  Lesson: a nested type with defaults cannot be a default argument of its own enclosing class.
- The Zig `captures` returned `Error!?Captures` while its body allocated, so the compiler rejected `error.OutOfMemory`.
  Lesson: in Zig, write the error union from what the body can produce, not from the domain error set alone.
- My first Zig iterator kept one scratch arena alive across steps and reset it between them.
  That needed a `deinit` on the iterator, which is easy to forget and which nothing else in the API needs.
  Giving each step its own arena, and copying the offsets out before it goes, removed the whole question.
  Lesson: a lifetime the caller must remember is a design cost.
  Pay one arena per step to avoid it.
- I claimed the Zig `Regex` serves any number of threads, in the same words as the other three targets.
  It does not, on its own: a search allocates its scratch arena from the allocator the caller gave `compile`, and a Zig allocator need not be thread safe.
  The other three reach a global allocator or the garbage collector, which are.
  The review caught it, and the claim now carries its condition.
  Lesson: a thread-safety statement covers every resource a call touches, and an injected allocator is one of them.
- The Zig `error_position` slot took a 0 when the failure had no position.
  Zero is a valid pattern offset, so the caller could not tell the two apart.
  It now leaves the slot as the caller set it.
  Lesson: do not encode "no value" as a value the domain already uses.
- The C++ `find_all` gave one arena to a whole iteration.
  Every match's search workspace stayed alive until the scan ended, because `vg::Arena` frees only when it dies.
  A 20000-match subject peaked at 167 MB against the 3 MB it needs.
  Rust and Zig got this right by accident of shape: their iterators build an arena per step.
  Lesson: an arena's lifetime is a decision, not a detail.
  Write down which call it serves, and check that a loop did not silently widen it.

## 2026-08-27, comment pass over every source file

- I rewrote comments with exact-match string replacements driven by a script, and one replacement in `go0/edge_test.go` kept failing with a count of zero while the target text looked identical on screen.
  The comment discusses the Kelvin sign, and it carries a real U+212A where I had typed an ASCII `K`.
  The two render the same in a terminal.
  Lesson: when an exact match fails on text that looks right, compare the code points before comparing anything else.
  A file that discusses Unicode is likely to contain it.
- My first pass at the Zig doc comments left a stray blank line inside a `///` block.
  `zig fmt` removed it on the next save, so the mistake cost nothing, but I only noticed because the harness reported that the file had changed under me.
  Lesson: read the formatter's result rather than assuming the edit landed as written.

## 2026-08-30, contributor guide

- My first `AGENTS.md` draft was 415 words, above the requested optimal range.
  I tightened repeated descriptions and brought the guide below 400 words.
  Lesson: check explicit size guidance as part of the first draft review.

## 2026-08-30, correctness audit

- I first used `find ..` to locate contributor guides even though the repository workflow requires `rg` for file searches.
  The parent traversal did not finish promptly and produced no useful result.
  I replaced it with `rg --files -g AGENTS.md`, which found the root guide immediately.
  Lesson: use the repository's required search tool before a broad filesystem traversal.
- My first Go race command allowed the test cache, so go0 reported cached results instead of a fresh execution.
  I repeated the race suite with `-count=1` before treating it as evidence.
  Lesson: force a fresh run when the point of a diagnostic is current runtime behavior.
- I looked for Zig's failing allocator in `std/testing/failing_allocator.zig`, but Zig 0.17 names that file `std/testing/FailingAllocator.zig`.
  The standard library export in `std/testing.zig` gave the correct path and API.
  Lesson: resolve standard library helpers from their public export before assuming a source filename.
- I ran `zig fmt` on generated engine files after regeneration, which broke the repository's byte-for-byte reproduction invariant.
  The formatter changed generated switch layout and trailing blank lines even though the generator output was authoritative.
  I restored the artifacts by regenerating them and limited direct formatting to hand-written Zig files.
  Lesson: never format a generated artifact separately from its generator.
- My first Zig allocation-failure workflow compiled an ASCII range under the Czech locale.
  That locale intentionally rejects ranges, so the unlimited baseline failed before the test reached allocator injection.
  I kept the locale lookup but changed the expression to one with literal repetitions.
  Lesson: an allocation harness must use inputs that are semantically valid under every option it combines.
- I started the full go1 race suite with Go's default ten-minute timeout.
  Race instrumentation made the fixed differential test exceed that limit, so the run ended without correctness evidence.
  I repeated it with an explicit longer timeout and a fresh test count.
  Lesson: set a measured timeout for known heavy suites before using instrumentation that multiplies their cost.
- My first json2zig regression fixture omitted the closing brace of a builtin expression.
  The JSON decoder rejected the fixture before the generator test could run.
  I corrected the fixture and reran the focused package test.
  Lesson: validate synthetic IR fixtures before using their failures to assess generated code.
- I ran `cargo fmt --check` over generated Rust engines even though their checked-in form comes directly from json2rust and was already not rustfmt-clean.
  The check produced a large irrelevant diff but changed no files.
  I limited formatting checks to hand-written Rust sources and kept regeneration identity authoritative for generated files.
  Lesson: exclude generated artifacts from source-formatter gates unless the generator itself promises formatter-stable output.
- My first downstream Rust privacy recheck linked `target/debug/librevera.rlib`, which cargo test had not refreshed after the fix.
  The stale pre-fix library still exposed `vg`, so the unsafe reproducer compiled and produced misleading output.
  I rebuilt the library target explicitly before repeating the compile-fail check.
  Lesson: verify the timestamp or rebuild the exact artifact passed through `--extern` in downstream compiler tests.
- I ran a focused Go formatter and test command from `go1` while still prefixing its file paths with `go1/`.
  The formatter failed before changing any files because those paths resolved one directory too deep.
  I repeated the command with paths relative to its working directory.
  Lesson: align command paths with the selected working directory before running a multi-file formatter.
- My first end-to-end loop-post fixture placed the call in the loop body instead of the three-clause loop's post position.
  That source could not exercise the printer branch under repair.
  I moved the call into the post clause before generating any evidence from the fixture.
  Lesson: inspect a regression fixture against the exact syntax node named by the defect before running it.
- I repeated the working-directory path error when inspecting the corrected loop-post fixture from `go1`.
  The leading `sed` path was repository-relative, so the chained command stopped before generation.
  I changed every path in the command to be relative to `go1` before retrying.
  Lesson: validate all paths in a chained command, not only the paths passed to the principal tool.
- I passed one Go source file to `vego2json`, but its documented input is a package directory.
  The exporter rejected the path before producing JSON.
  I moved the fixture into its own temporary package directory and used that directory as input.
  Lesson: reread a command's usage contract before turning a unit regression into an end-to-end check.
- I tried to apply the synthetic-name fix as one large multi-file patch using an inaccurate C++ composite-expression context.
  Patch validation rejected the whole change, so no source file was modified.
  I split the change into smaller patches after reading the exact current regions.
  Lesson: use narrow patches when several generators need similar but structurally different edits.
- I compiled a minimal generated Rust fixture with `-D warnings`, although the generator unconditionally imports its runtime and the fixture did not use it.
  The expected unused-import warning became an unrelated hard error after the C++ and Zig fixtures had already compiled.
  I repeated the Rust compile without promoting warnings, matching the repository's generated-code build policy.
  Lesson: do not impose a stricter warning policy on a synthetic generated file than the project applies to generated artifacts.
- The validation specialist first ran `gofmt` from `go1` with paths that still began with `go1/`.
  It failed before modifying files, and the specialist repeated it with working-directory-relative paths.
  Lesson: pass formatter paths in the same coordinate system as the command's working directory.
- The first keyed-constant regression also used an array index key, which the Vego subset intentionally excludes.
  That unrelated rejection obscured the struct-value traversal bug under test.
  The specialist narrowed the fixture to an unkeyed array containing a keyed struct literal.
  Lesson: make a boundary fixture contain only the one construct whose acceptance is being tested.
- My Rust keyword probe read source from standard input without selecting an output path.
  `rustc` left its default `librust_out.rlib` in the repository root, outside the required temporary directory.
  I removed that disposable artifact immediately and kept subsequent outputs under `tmp`.
  Lesson: even syntax-only-looking compiler probes need an explicit no-output mode or a path in `tmp`.
- I appended `zig build test -Doptimize=ReleaseFast` to a root-level bounds probe without changing into `zig1`.
  The ReleaseFast probe itself correctly panicked, but the suite command could not find `build.zig`.
  I reran the suite from the Zig component directory.
  Lesson: give each command in a multi-stage validation the working directory of the component it builds.
- My corrected-directory Zig command still used the older `-Doptimize=ReleaseFast` build option spelling.
  Zig 0.17 rejected that project option during configuration.
  I initially misread the short error and wrote `-Drelease=fast` as the replacement.
  The full help distinguishes the Boolean project option `-Drelease` from the general `--release=fast` option, which is the correct spelling for an explicit mode.
  Lesson: read the full active Zig build help instead of inferring an option's form from the short error list.
- The go1 specialist initially called `constant.Int64Val` before checking whether a source constant was actually an integer.
  String and Boolean constants could reach that path and panic.
  The specialist checked the `go/types` constant kind first, then added integer-specific validation.
  Lesson: arbitrary-precision conversion helpers still require a type-kind guard at an untyped source boundary.
- The go1 specialist submitted one duplicate-target patch while assembling its second change set.
  Patch validation rejected it without modifying files.
  The duplicate target was removed before applying the intended edit.
  Lesson: inspect a multi-file patch for repeated update headers before submitting it.
- The first arbitrary-precision validation ran recursively inside `retype`.
  It rejected the valid intermediate `1 << 63` in `(1 << 63) - 1` and tried to fold nonconstant engine expressions.
  Moving representability validation to `defaultType`, where the complete untyped constant freezes, fixed both cases.
  Lesson: validate a constant's bounds only at the semantic point where its final default type is chosen.
- I again inspected a repository-relative `go1/...` path while running the command from inside `go1`.
  The leading `sed` failed, and the chained focused tests did not start.
  I repeated both operations with paths relative to the selected working directory.
  Lesson: avoid switching a long-running audit between root-relative and component-relative path conventions.
- The printer specialist began a tuple-name collision reproducer from a stale view while the checker specialist was concurrently reserving the `Tup_` namespace.
  Rereading the shared diff showed that the current checker already rejected the fixture.
  Lesson: revalidate a candidate against the shared worktree before investing in a reproducer during concurrent edits.
- The printer specialist ran `go test` on a fixture directory after generated C++ files had been placed beside its Go oracle.
  Go rejected the unrelated generated files before running the intended test.
  The specialist reran the oracle by naming only its Go source and test files.
  Lesson: keep mixed-language fixture outputs out of a package-level Go test target.
- The first compound-assignment oracle made the shared marker return zero for the divisor.
  The Go program panicked before it could measure evaluation order.
  Returning `value - 1` kept the index at zero while making the divisor nonzero.
  Lesson: validate all arithmetic preconditions in a side-effect-order oracle.
- A combined three-printer test patch used C++ context that had changed under concurrent keyword work.
  Patch validation rejected it without modifying files.
  The specialist reread each test file and applied smaller independent patches.
  Lesson: split concurrent shared-file changes into narrow patches with current context.
- The first Zig loop-post pin used a named block whose label was never referenced.
  Zig rejected the unused label in the compile-backed regression.
  The specialist retained the block expression but removed the unnecessary label.
  Lesson: add a Zig block label only when a `break` expression targets it.
- I launched the full crosscheck inside a parallel orchestration call but printed only each command's text output.
  The crosscheck crossed the initial yield boundary, and I discarded its returned session identifier before the final driver reports arrived.
  I reran it in a dedicated command that preserved and polled any live session.
  Lesson: print or store the complete result object for any command that can outlive an orchestration call.
- My first patch mirroring reserved package names into `vego2json` assumed the package-variable loop built its JSON entry before its validation checks.
  The current function performs constant-data and slice checks first, so the context did not match and the whole patch was rejected.
  I reread each declaration handler and applied the shared name check at its actual entry point.
  Lesson: patch structurally similar declaration loops independently when their validation order differs.
- The final command audit initially put two Go packages in one temporary round-trip fixture.
  Go rejected the mixed package directory, so the specialist split the fixture by package.
  Lesson: keep every temporary Go package in its own directory.
- A Python wrapper in the final command audit over-escaped newline characters and wrote literal backslash sequences.
  The specialist corrected the fixture to write actual protocol lines.
  Lesson: inspect generated fixture bytes when testing line-oriented protocols.
- The final command audit used an incorrect absolute path for the macOS `true` utility.
  The specialist resolved the installed command before retrying.
  Lesson: discover utility paths instead of assuming a platform layout.
- The final command audit attempted a broad recursive removal for temporary cleanup, which the safety policy rejected.
  The specialist removed only the explicit temporary fixture targets.
  Lesson: clean temporary artifacts with narrow, resolved paths.
- I regenerated the Lean corpus after adding differential cases and updated its count, but I did not rerun the formal agreement theorem before describing the corpus as current in the progress log.
  The subsequent full Lean build found that `corpusAgrees = true` was false.
  Lesson: a regenerated corpus is not validated until the theorem checks every new row against the formal model.
- When I updated the Lean corpus totals, I searched for the old total and coverage figures but missed the old compile count and a replay total in `lean/PROGRESS.md`.
  A repository-wide count search found and corrected both stale values.
  Lesson: after changing a generated corpus, search every documentation file for all derived counts, not only the headline total.
- My first Lake dependency probe tried to append an empty line to a file that already ended with a newline.
  The patch produced no byte change, so it could not test the dependency trace.
  I repeated the probe with a visible trailing-space line, verified the rebuild, then restored the file exactly.
  Lesson: confirm a temporary input mutation with `git diff` before interpreting a build-cache result.
- I repeated an already-recorded cleanup mistake by invoking `rm -rf` on the resolved audit directory.
  The safety wrapper rejected it without removing anything.
  I switched to a non-forcing recursive removal of that one verified directory.
  Lesson: use the narrow non-forcing cleanup form even when the recursive target has already been resolved.
- My final hygiene sweep ran crate-wide Rust and source-wide Zig formatter checks even though the target printers, not those formatters, own the generated engine files.
  Both checks reported existing style differences and changed nothing.
  I narrowed the checks to the hand-written files changed by this audit.
  Lesson: exclude authoritative generated artifacts from formatter checks unless the generator promises canonical formatter output.
- I printed two overlapping ranges of the new generator source and mistook their shared boundary line for a duplicated unreachable return.
  My attempted correction had impossible context, so patch validation rejected the entire edit without changing files.
  I reread the exact line numbers and confirmed that the source contained only one return.
  Lesson: account for overlap when concatenating adjacent source excerpts.
- The first transactional installation path ignored an error while restoring the current artifact after a failed rename.
  I changed the path to join the restoration error with the original installation failure.
  Lesson: rollback failures are correctness evidence and must never be discarded.
- The generation-audit specialist used a zsh redirection combination that enabled multios and mixed status output into hash pipelines.
  Repeating the hash checks through a plain shell established that the artifacts and locale blobs matched.
  Lesson: isolate redirected hash pipelines from shell-specific multios behavior.
- I ran a generator inspection and formatting command from `go1` but gave those two steps repository-relative `go1/...` paths.
  They failed before changing the file, while the following package test and vet still ran because the command did not stop on error.
  I repeated the inspection and formatter with component-relative paths.
  Lesson: use one path base consistently and stop compound validation commands when an early prerequisite fails.
- Two root-discovery tests used directories under the real repository's `tmp/` tree as supposedly external paths.
  Root discovery correctly walked through their ancestors and found the real repository, so both expectations were wrong.
  I changed those cases to start above the actual repository root.
  Lesson: a negative ancestor-search fixture must not be nested under the object it claims is absent.
- The first live freshness test reported all ten artifacts stale after the tracked generator binary was removed.
  I stopped before regenerating and compared exact output to determine whether Go VCS stamping or the new orchestration plan caused the difference.
  Lesson: diagnose a repository-wide deterministic-output mismatch before accepting generated rewrites.
- The faulty negative-root CLI test did more than assert the wrong exit code.
  It ran `generate` with a fake producer against the real repository root discovered above its `tmp/` fixture, replacing all ten authoritative artifacts with sentinel text.
  Direct comparison proved the mutation, and I fixed the test to start above the real repository before restoring every artifact through the real generator.
  Lesson: never give a mutating fake-runner test a path whose ancestors include the live repository; assert the resolved root before execution.
- The first unified-workflow implementation checked only artifact leaf symlinks.
  A symlink in an artifact's parent path could redirect comparison and installation outside the repository.
  Lesson: validate every repository-relative directory component with `Lstat` before reading or replacing generated artifacts.
- Repository discovery matched only an LF byte sequence after the Go module declaration.
  A valid CRLF checkout was therefore rejected.
  Lesson: isolate and normalize the declaration line when a text format permits platform line endings.
- The no-binary regression used filesystem existence as a substitute for Git tracking state.
  An unrelated untracked local build would fail the test even though the repository policy was satisfied.
  Lesson: do not claim to test version-control metadata through ordinary filesystem calls.
- My first component-walk patch normalized repository paths only with `filepath.Clean`.
  Forward-slash artifact paths would remain unsplit on Windows, allowing a parent symlink to evade the per-component check.
  Lesson: convert slash-separated repository paths with `filepath.FromSlash` before walking native path components.
- I launched the post-hardening cross-build from the repository root, which is not a Go module.
  The command stopped before compiling either target.
  Lesson: run Go builds from `go1` or select that module explicitly through an appropriate workspace configuration.
- The first staged-output hardening patch redeclared `info` and `err` in a scope where both names already existed.
  Go rejected the function before tests ran.
  Lesson: after adding an earlier inspection in the same loop, reuse existing variables with assignment.
- I prepended `zig build` to a conformance command running from `go1`.
  Zig correctly stopped because that directory has no `build.zig`.
  Lesson: build a target in its component directory before returning to `go1` for the shared conformance runners.

## 2026-08-31, Claude Code setup

- I ran the go1 lint baseline without a `cd go1`, so the shell was still in `go0` and I read the go0 results twice.
  Lesson: use absolute paths or repeat the `cd` in every compound command; the working directory carries over between tool calls.
- The first `.golangci.yml` exclusion used `^probe/`, but a root config reports paths relative to the repository, so `go1/probe/` never matched.
  Lesson: check the paths in the lint output before writing a path rule.
- I nearly configured the format hook with `rustfmt <file>`, which follows `mod engine;` and reformats the generated engine.
  Lesson: run `rustfmt` on stdin, or only on files that declare no out-of-line modules.
