# Changelog

All notable changes to Revera are recorded here.
The format follows Keep a Changelog, and the project follows Semantic Versioning.
The engine release number is shared by the Go, Rust, Zig, C, C++ and TypeScript implementations.
The Vego toolchain has its own version, which `vegoc version` prints.

## Unreleased

### Fixed

- One-pass expressions no longer report the general capture solver or include its unreachable worst-case figures in their totals.
  The compile-time backend choice is now exclusive in both the contract and execution.
  If the one-pass walk ever detects an internal inconsistency, execution fails closed with the existing capacity error instead of entering an unreported solver path.
  For `(abc+)` with a 1000-byte subject limit, the Rust contract now reports 37,757 heap bytes and 925,986 steps instead of roughly 413 GB and 101 billion steps.
- Expressions without parenthesized subexpressions no longer report a capture backend or include unreachable Phase B costs in their resource contracts.
  The canonical and reference contract validators now reject one-pass or solver figures for those expressions.
- Resource contracts price a bracket test from the lookups it really performs: a binary range search, one class lookup, and one primary comparison per equivalence class, plus one probe per multi-character length the bracket can match.
  The former figure charged every bracket for sixteen case preimages, and it still fell short of the collating searches an equivalence class runs in a full locale.
  Case-sensitive brackets now cost a few units per test, while equivalence classes in a full locale report the thousands of units their searches take.
  The stack figures also gained the frames of the deepest lookup a bracket test can start, which a multi-character probe in a full locale exceeded by two frames.
  The corpus gained locale runs of such brackets on long subjects, so the Lean replay keeps those figures honest.
- The matcher step figure of a start-anchored expression of bounded length no longer charges one boundary per subject byte.
  Such an expression, compiled without newline mode, seeds no thread past the first boundary, and its scan filter jumps to the end of the subject once the threads of the first boundary are gone.
  The compiler now records the longest consuming path of the program as its depth, and the contract charges depth+3 boundaries when that is fewer.
  For `^abc$` on a subject of at most 1000 bytes the figure drops from 1,222,246 steps to 8,346.
  `lean/Vego/PhaseAAnchored.lean` proves the bound for every program that carries a decidable certificate, the corpus link builds that certificate for every anchored contract it checks, and the corpus gained five anchored patterns to exercise it.
- A pattern that nests parentheses more than 256 deep now fails to compile with a capacity error.
  The parser and every later pass recursed once or more per level, so such a pattern exhausted the stack instead.
  In C, 1,500 unbalanced parentheses crashed a default macOS thread, and a long enough pattern killed a Go program with an unrecoverable stack overflow.
  No pattern of 256 bytes or fewer reaches the limit.
- Resource contracts no longer underestimate steps.
  The bracket, locale lookup and one-pass prices missed the final failing test of each loop, which the Lean meter counts, and several calls around them.
  On adversarial but valid patterns the measured steps passed ContractSteps: 83.9 million against 83.7 million for 2,000 POSIX equivalence classes, and 24.3 million against 18.6 million for 999 Hungarian collating elements.
  The size-capped fallback and programs with a pruned subtree now also pay for the character count Exec runs first, which costs up to three units per byte of invalid UTF-8.
  A program whose pruned subtree can match the empty string no longer reports a capture backend that can never run.
- C and C++ runtime checks no longer use `assert`, so a build that defines `NDEBUG`, such as a CMake Release build, keeps them.
  Array indexes, unsigned divisors, allocation sizes and shift counts are now checked as well, and each aborts where Go would panic.
  A shift count outside the width of its operand now aborts in C, C++, Rust release builds and TypeScript, as it does in Zig and in the Vego semantics.
- The Go locale loader rejects a case profile above 1 or a default collation past the last profile, instead of checking the stored value only after narrowing it.
- A failed `rv_locale_open` in the C locale runtime leaves its result as it was, instead of half a new locale that could index past the collation profiles.
- TypeScript: a truthy `noCaptures` other than `true` no longer makes `find` and `captures` return made-up matches, and `replaceAll` accepts integer limits beyond the safe range.
- Zig: `CaptureIterator.next` no longer loses a match when the allocation of its group list fails and the caller retries.
- Vego: `vegoc check` and `vegoc export` now run the checks of `vegoc emit` too, and they reject programs whose translations could disagree with Go.
  Those include a read of storage before a call that writes it in the same statement, two-value assignments whose second target reads the first, and ranges over indexed arrays that Go does not evaluate.
  They now also enforce rules 3 and 4 of the buffer model for views: a view of a local array is never returned or kept past its block, and neither such a view nor a slice or pointer parameter is stored in a field, an element, a composite literal or an append.
  In C, AddressSanitizer showed such programs reading a stack array after its block or function had ended.
  They also reject string literals that are not valid UTF-8, declarations that hide `true`, `false`, `nil` or a scalar type, a `:=` that reuses a variable, pointer locals, `&` inside a slice element, and non-ASCII names.
  Constant expressions now fold under Go's typing rules: an untyped `^` no longer truncates at the context type, and a constant declared from a typed value, such as `const w = uint16(3)`, stays typed.
  The Zig printer keeps the Go type of `min` and `max`, and it compiles several valid forms it used to reject.
  The TypeScript printer copies value arguments, returned globals and whole stores as Go does, and orders classes before the globals that use them.
  The C++ and Zig printers no longer crash on an array length computed from constants, and the Rust printer no longer turns every unary operator in such a length into a minus, which gave `[^k]int64` the wrong length.
- The conformance kit compares `ts/src/data.bin` with the other copies of the locale data, which it had skipped.
- `dist` reads raw blobs from the recorded commit, so `core.autocrlf` or `.git/info/attributes` can no longer change the archives, and the manifest records the `vegoc` version of that commit.
  Its `-out` option now refuses a directory that holds anything but earlier release assets, instead of deleting it.
- The CI license check now fails when a license copy is missing from the commit.
- The C and C++ protocol drivers decode uppercase hex like the other drivers.

### Added

- The Revera engine, a clean-room POSIX.1-2024 ERE implementation written in Vego and generated for Go, Rust, Zig, C, C++ and TypeScript.
- The Vego language: the specification, the structural schema of the IR, the compiler and the `vegoc` command.
- The Lean 4 model of Vego and of the ERE specification, with the corpus and specification checks and theorems for Phase A, the match-span stage.
- The backend conformance kit, the cross-language benchmarks and the fuzz drivers, in the unpublished `dev` module.
- The Go modules `github.com/oneregex/revera/go` and `github.com/oneregex/revera/vego`, the `revera` crate, the `revera` Zig package, the `@oneregex/revera` npm package and the `revera` CMake package with `Revera::C` and `Revera::CXX`.
