# Changelog

All notable changes to Revera are recorded here.
The format follows Keep a Changelog, and the project follows Semantic Versioning.
The engine release number is shared by the Go, Rust, Zig, C, C++ and TypeScript implementations.
The Vego toolchain has its own version, which `vegoc version` prints.

## 0.1.0 - unreleased

The date replaces `unreleased` when the tag is made; `make dist` checks for it.

### Fixed

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

### Added

- The Revera engine, a clean-room POSIX.1-2024 ERE implementation written in Vego and printed into Go, Rust, Zig, C, C++ and TypeScript.
- The Vego language: the specification, the structural schema of the IR, the compiler and the `vegoc` command.
- The Lean 4 model of Vego and of the ERE specification, with the corpus and specification checks and theorems for Phase A, the match-span stage.
- The backend conformance kit, the cross-language benchmarks and the fuzz drivers, in the unpublished `dev` module.
- The Go modules `github.com/oneregex/revera/go` and `github.com/oneregex/revera/vego`, the `revera` crate, the `revera` Zig package, the `@oneregex/revera` npm package and the `revera` CMake package with `Revera::C` and `Revera::CXX`.
