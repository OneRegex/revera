# Repository Guidelines

## Project Structure & Module Organization

`src/` and `tests/` hold the C11 locale runtime and its tests.
`go0/` is the reference POSIX ERE engine.
`go1/` contains the Vego implementation, translators, and cross-language verification tools.
`rust1/`, `zig1/`, and `cpp1/` combine generated engines with hand-written APIs.
Lean proofs are under `lean/Vego/`, with fixtures in `lean/data/`.
Design notes live in `docs/`.
Change pinned projects in `ref/` only when updating a reference revision.

Locale blobs such as `*/data.bin` are embedded assets.
Do not hand-edit generated files, including `src/rv_locale_data.inc`, `go1/*.vego.json`, `rust1/src/engine.rs`, `zig1/src/engine.zig`, and `cpp1/engine.{hpp,cpp}`.
Use the regeneration commands in the component READMEs.

## Build, Test, and Development Commands

- `make test`: build and run the C locale tests with warnings treated as errors.
- `cd go0 && go test ./...`: test the reference engine and locale package.
- `cd go1 && go test ./...`: test Vego validation, translators, and Go output.
- `cd rust1 && cargo test`: test the Rust API.
- `cd zig1 && zig build test`: run the Zig API tests.
- `cd cpp1 && make test`: build and run the C++20 API test.
- `cd lean && lake build`: check the Lean model and proofs; expect several minutes.

For generated-target changes, build release drivers and run `go run ./cmd/crosscheck ...` and `go run ./cmd/probecheck ...` from `go1/`.

## Coding Style & Naming Conventions

Use native formatters and existing language idioms.
Go code must be clean under `gofmt` and `go vet`.
C uses four-space indentation, `rv_` functions, and `RV_` constants.
Rust uses `snake_case`, Zig methods use `camelCase`, and C++ methods use `snake_case`.
Keep comments concise and useful.

## Testing Guidelines

Add tests beside the affected component using its native convention: `test_*` in C, `*_test.go` with `TestXxx` in Go, `#[test]` in Rust, descriptive `test "..."` blocks in Zig, and focused helpers in C++.
There is no numeric coverage threshold.
Conformance, differential behavior, resource bounds, overflow semantics, and thread safety are the important evidence.

## Commit & Pull Request Guidelines

History favors short, specific subjects such as `Simplify contract proofs` and `Add api_test.go`.
Describe only what the change does.
Pull requests should identify affected components, explain generated-file changes, link relevant issues, and list exact verification commands.
Screenshots are unnecessary unless output presentation changes.
Keep `LOG.md` current; record corrections in `MISTAKES.md` and Zig API surprises in `api-faq.md`.
