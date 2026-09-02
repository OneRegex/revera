# Changelog

All notable changes to Revera are recorded here.
The format follows Keep a Changelog, and the project follows Semantic Versioning.
The engine release number is shared by the Go, Rust, Zig, C, C++ and TypeScript implementations.
The Vego toolchain has its own version, which `vegoc version` prints.

## 0.1.0 - unreleased

The date replaces `unreleased` when the tag is made; `make dist` checks for it.

### Added

- The Revera engine, a clean-room POSIX.1-2024 ERE implementation written in Vego and printed into Go, Rust, Zig, C, C++ and TypeScript.
- The Vego language: the specification, the structural schema of the IR, the compiler and the `vegoc` command.
- The Lean 4 model of Vego and of the ERE specification, with the corpus and specification checks and theorems for Phase A, the match-span stage.
- The backend conformance kit, the cross-language benchmarks and the fuzz drivers, in the unpublished `dev` module.
- The Go modules `github.com/oneregex/revera/go` and `github.com/oneregex/revera/vego`, the `revera` crate, the `revera` Zig package, the `@oneregex/revera` npm package and the `revera` CMake package with `Revera::C` and `Revera::CXX`.
