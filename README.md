# Revera

One POSIX regex engine in Go, Rust, Zig, C and C++, cross-checked for one behavior and backed by machine-checked Lean semantics and proofs.

Revera is a clean-room implementation of the POSIX.1-2024 Extended Regular Expression language.
The engine is written once, in Vego, a strict Go subset built for mechanical translation.
A compiler exports it as Vego IR, and printers turn that IR into the Rust, Zig, C++ and C engines.

As a result, all five libraries come from one engine source.
The conformance corpus cross-checks their matches, errors and resource-contract reports.

The Lean 4 model provides the formal side of the project.
It proves the shipped IR well formed.
It also checks the interpreted engine against a formal ERE model on the constrained corpus cases and an exhaustive small domain.
In addition, it proves phase A properties under the stated hypotheses.

Finally, the engines embed generated CLDR and Unicode tables for character classes, case mappings and collating data.
The `locale/` directory contains the C runtime and the generator used to reproduce and verify those tables.

## The libraries

| Language | Package                                      | Directory     |
| -------- | -------------------------------------------- | ------------- |
| Go       | `github.com/oneregex/revera/go`              | `go/`         |
| Rust     | crate `revera`                               | `rust/`       |
| Zig      | package `revera`                             | `zig/`        |
| C        | `Revera::C`, `librevera_c`, `revera-c`       | `native/c/`   |
| C++      | `Revera::CXX`, `librevera_cxx`, `revera-cxx` | `native/cpp/` |

Each directory has a README with the API and the verification commands of that language.
The Go engine is the canonical source.
The generated engine files of the other languages keep their names everywhere: `engine.rs`, `engine.zig`, `engine.hpp` with `engine.cpp`, and `engine.h` with `engine.c`.

## Repository contents

- `revera.vego.json` is the engine as Vego IR, the release artifact from which every generated backend starts.
- [`go/`](go/) is the canonical Vego source of the engine, plus its Go host files.
- [`vego/`](vego/) is the language: [`SPECIFICATION.md`](vego/SPECIFICATION.md), the structural schema of the IR, the compiler, the `vegoc` command, and the probe program that a Vego backend must reproduce.
- [`rust/`](rust/), [`zig/`](zig/) and [`native/`](native/) are the engine printed into each target language, with a hand-written public API in the shape that language expects.
  `native/` is one CMake package with the C and C++ libraries.
- [`lean/`](lean/) is the Lean 4 model of Vego and the formal model of the ERE specification, with the theorems and the corpus replay tools.
- [`locale/`](locale/) is the C locale runtime, the CLDR tables, their tests and their generator.
- [`dev/`](dev/) is the development module: the reference engine, the conformance kit, the generator, the benchmarks and the fuzz drivers.
  It is never published.
- [`docs/`](docs/) holds the specifications and the reports:
  [`POSIX-1-2024-ERE-SPECIFICATION.md`](docs/POSIX-1-2024-ERE-SPECIFICATION.md) is the contract every engine implements;
  [`TRE-POSIX-ERE-DIVERGENCES.md`](docs/TRE-POSIX-ERE-DIVERGENCES.md) records where TRE differs from it;
  [`ERE-IMPLEMENTATION-TECHNIQUES.md`](docs/ERE-IMPLEMENTATION-TECHNIQUES.md) collects techniques from TRE, RE2 and MinRX;
  [`LOCALE-TABLES.md`](docs/LOCALE-TABLES.md) documents the locale model;
  [`CONFORMANCE-KIT.md`](docs/CONFORMANCE-KIT.md) describes the backend conformance kit;
  [`BENCHMARKS.md`](docs/BENCHMARKS.md) describes the benchmarks and records the figures.
- [`third_party/`](third_party/) holds pinned upstream trees as submodules, for study only: TRE, RE2 and MinRX.

## What is proved

The Lean development proves both IR files well formed and proves that the interpreted probe reproduces the Go report.

Its corpus theorem compares the interpreted engine with recorded Go answers on the embedded replay set and meters the retained Exec commands against their contracts.
Replacement and iteration commands are checked for output agreement only, and exceptionally expensive Exec commands are excluded.

Separately, the development states the ERE specification as a Lean definition.
It checks the interpreted engine against that definition on every constrained corpus command and on an exhaustive small domain.

For phase A, the heap and step bounds are proved for every well-formed program, atom test and subject.
The reported match is also proved correct under the additional hypotheses stated in the formalization.
However, the link to the shipped engine covers only corpus executions that use phase A alone.

[`lean/README.md`](lean/README.md) states each theorem and what it does not cover.

## Build and test

```sh
git clone --recurse-submodules https://github.com/oneregex/revera
cd revera
make test                          # the C locale runtime
(cd go && GOWORK=off go test -count=1 ./...)
(cd vego && GOWORK=off go test -count=1 ./...)
(cd dev && go test -count=1 -timeout 30m ./...)
(cd rust && cargo test --workspace)
(cd zig && zig build test)
(cd lean && lake build)
cmake -S native -B tmp/native-build -DBUILD_TESTING=ON \
  && cmake --build tmp/native-build \
  && ctest --test-dir tmp/native-build --output-on-failure
```

The two Go modules are tested with `GOWORK=off`, because the workspace file would hide a missing dependency.

Regenerate the IR and every generated engine with one command, and check that they are current with another:

```sh
make generate
make check-generated
```

`check-generated` renders into an isolated directory under `tmp/`, compares file contents, and exits nonzero without touching a checked-in file when one is stale or missing.
Set `GENERATION_TARGETS=rust`, `zig`, `cpp`, `c`, or a comma-separated selection to limit the target sources.
The two IR files are always included.

One command runs the conformance suite for every generated backend, and one command benchmarks every engine:

```sh
make conform
make bench
make size
make profile
```

`conform` builds each generated backend from its `backend.json`, runs the release probe, differential corpus, stress rounds and fuzz seed pack, and verifies that the checked-in Lean data is current.
It attempts each manifest's configured debug or sanitizer builds and repeats the light checks when their toolchains are available.

`make conform CONFORM_FLAGS="-lean"` also builds the Lean development, runs `vegocheck` on the Lean-covered corpus commands, and runs `speccheck` on the specification-constrained commands.

`bench` builds every release backend, then prints compile, match, replacement and difficult-pattern figures with allocation counts.
`size` reads those release drivers and reports engine source and machine-code sizes, so run it after `make bench` or `make conform`.
Finally, `profile` writes CPU and allocation profiles of the Go engine.

`make licenses` synchronizes `LICENSE` and `LICENSES/Unicode-3.0.txt` into `go/`, `rust/`, `zig/` and `native/`.
Before a release, replace `unreleased` in the version's changelog heading with its date, run `make licenses`, and commit the result.

`make dist` then reads committed `HEAD` and refuses tracked changes.
It checks the Cargo, Zig and CMake versions as well as the license copies.
Finally, it stages deterministic Zig and native archives, both IR files, and a manifest with the source commit, `vegoc` version, IR digest and asset checksums in `tmp/dist/`.

## Versions

Three axes move on their own:

- the Revera engine release, one number shared by the five implementations, starting at `0.1.0`;
- the Vego toolchain, its own SemVer, starting at `0.1.0`, which `vegoc version` prints;
- the Vego IR compatibility major, the `"vego": 1` field of the IR.

Engine releases use matching project `vX.Y.Z` and Go engine `go/vX.Y.Z` tags.
Vego releases use independent `vego/vA.B.C` tags; the first engine and Vego releases both happen to be `0.1.0`.
[`CHANGELOG.md`](CHANGELOG.md) records the releases, and [`SECURITY.md`](SECURITY.md) says how to report a vulnerability.

## License

Revera is licensed under the MIT license, in [`LICENSE`](LICENSE).

The MIT license covers the Vego source, the Vego IR, the generated engines in every language, the hand-written runtimes and the binaries built from them.

The embedded Unicode and CLDR data is under the Unicode License v3, in [`LICENSES/Unicode-3.0.txt`](LICENSES/Unicode-3.0.txt).
Therefore, every independently shipped Revera engine package carries a copy of both files.

By contrast, the Vego toolchain contains no Unicode or CLDR data.
