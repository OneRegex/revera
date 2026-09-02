# Development module

This module, `github.com/oneregex/revera/dev`, contains the repository's development and release infrastructure.
It is not published for users.

Within the repository, the module depends on the `go` and `vego` modules through the `replace` directives of its `go.mod`, and the root `go.work` ties all three modules together.

## Commands

Run them from this directory.
Each one finds the repository root from the working directory, or takes `-repo`.

- `go run ./cmd/generate [-check] [-target rust,zig,cpp,c|all]` renders the Vego IR and the generated engines with `vegoc`, into a staging directory under `tmp/`, and installs them in one transaction.
  With `-check` it compares the staged output with the checked-in files and installs nothing.
  `make generate` and `make check-generated` at the root call it.
- `go run ./cmd/conform [-backend dir]... [-stress rounds] [-seed n] [-quick] [-skip steps] [-lean] [-allow-skip] [-timeout duration]` runs the backend conformance kit.
  `make conform` calls it.
- `go run ./cmd/bench [-backend dir]... [-reps n] [-scale factor] [-reference] [-build] [-only prefix] [-tsv file]` runs the shared benchmark cases on every engine.
  In addition, `go run ./cmd/bench size [-backend dir]...` reports the generated code sizes.
  `make bench` and `make size` call these commands.
- `go run ./cmd/dist [-commit ref] [-allow-dirty] [-out dir]` stages the release assets; see below.

## Packages

- `internal/protocol/` is the Go side of the line protocols and the shared cases that every target implements.
  It holds the driver session, the bench cases and session, the corpus tables, and the fuzz input format with its seed pack.
  Every target's `driver`, `bench` and `fuzzcase` binary mirrors these files.
- `internal/conformance/` is the kit: the corpus builder, the driver and probe runners, the fuzz seed pack, the manifest loader and the step pipeline.
  [`../docs/CONFORMANCE-KIT.md`](../docs/CONFORMANCE-KIT.md) describes it.
  Its subdirectories hold standalone diagnostic commands and reporting helpers.
  `godriver` speaks the driver and bench protocols with the Go engine, while `proberef` prints the probe report and `fuzzcase` replays a seed pack.
  In addition, `crosscheck` compares drivers with the Go engine, `probecheck` compares probe binaries, `bench` contains the benchmark logic, and `codesize` reads machine-code sizes from a binary.
- `internal/reference/` is the reference engine, the first Go implementation of the specification.
  An enumerating matcher and the host `regcomp()` check it, and the Revera engine is checked against it.
  It sits under `internal` so that nobody mistakes it for a second supported Go implementation.
- `internal/differential/` compares the Revera engine with the reference engine: random and fixed pattern corpora, every flag, locales with multi-character collating elements, replacement and iteration.
  It also regenerates `../go/testdata/locale-expected.tsv.gz`, the locale answers that the engine module tests replay.
- `internal/genlocale/` builds `data.bin` from `../locale/rv_locale_data.inc` and writes every checked-in copy.

## Verification

```sh
go test ./...
go test -count=1 -timeout 30m ./internal/differential
go test -count=1 -timeout 30m -race ./internal/differential
go test ./internal/differential -run TestLocaleExpectedData -update
go run ./internal/conformance/crosscheck ../rust/target/release/driver ../zig/zig-out/bin/driver ../native/cpp/driver ../native/c/driver
go run ./internal/conformance/probecheck ../rust/target/release/probe
```

The differential suite is heavy, so give it `-count=1` and an explicit `-timeout`.

`crosscheck` and `probecheck` need release drivers; debug builds are far too slow for the corpus.
`bench size` builds the Go driver itself but expects every selected backend's release driver to exist, so run `make bench` or `make conform` first.

## Release staging

`go run ./cmd/dist`, or `make dist` at the root, stages the release assets in `tmp/dist/`.
The assets are the Zig package archive, the native package archive and the two IR files.
The manifest records the source commit, the `vegoc` version, the IR digest and the checksum of every staged asset.

To make the output reproducible, the command reads every file from the commit it records and writes archives with sorted members, fixed modes and ownership, and timestamps normalized to the commit time.
As a result, two runs on the same commit produce the same bytes.

The command refuses a dirty tracked tree unless `-allow-dirty` is given.
It also refuses to run when the three package versions disagree, when a license copy is stale, or when `CHANGELOG.md` has no dated section for the version.

However, it does not regenerate or test the committed files, so run `make check-generated` and the release validation before committing the revision to stage.

Separately, `sh sync-licenses.sh` copies the root license files into `go/`, `rust/`, `zig/` and `native/`.

## License

The development code is licensed under the repository's [MIT license](../LICENSE).
The embedded reference locale data is covered by the [Unicode License v3](../LICENSES/Unicode-3.0.txt).
