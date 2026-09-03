# Backend conformance kit

A backend is one instantiation of the Vego engine: a printer output plus a hand-written runtime, driver, probe and public API.
This kit decides whether a backend is conformant with one command.
It packages the probe protocol, the corpus generator, the differential runner, the stress rounds, the fuzz entry points, the checked builds, and the Lean replay.

The kit lives in `dev/internal/conformance`.
The command is `dev/cmd/conform`, and the root Makefile wraps it:

```sh
make conform
make conform CONFORM_FLAGS="-backend ../native/cpp"
cd dev && go run ./cmd/conform -backend ../rust -stress 50 -lean
```

The exit status is 0 only when every step passed.
The last lines give one verdict per backend.

## What a backend must provide

Each backend directory holds a `backend.json` manifest.
The kit discovers every `*/backend.json` below the repository root and every `native/*/backend.json`, or takes the directories named with `-backend`.

```json
{
  "name": "cpp",
  "generated": ["engine.hpp", "engine.cpp", "probe_engine.hpp", "probe_engine.cpp"],
  "engine_symbols": "6revera6engine",
  "release": {
    "build": [["make", "all"]],
    "driver": "driver",
    "probe": "probe",
    "bench": "bench",
    "fuzzcase": "fuzzcase"
  },
  "checked": [
    {
      "name": "asan-ubsan",
      "build": [["make", "sanitize"]],
      "driver": "driver-asan",
      "probe": "probe-asan",
      "fuzzcase": "fuzzcase-asan"
    }
  ]
}
```

- `name` identifies the backend in reports.
- `generated` lists the generated sources, for the code-size report of `dev/cmd/bench size`.
- `engine_symbols` is a regular expression over symbol names in the release driver.
  The code-size report sums the machine code of the functions it matches.
  A backend that runs under an interpreter leaves it out, and the report then gives its source size only.
- `toolchain` is a command that prints the compiler version, for the header of the benchmark report.
- `release` is the optimized build.
  `build` is a list of argument vectors, run in order in the backend directory.
  The paths are relative to the backend directory.
  `driver` and `probe` are required.
  `bench` is used by `dev/cmd/bench`, and `fuzzcase` by the fuzz step.
- `checked` lists builds with runtime checks: sanitizers, debug modes, or a checked optimization level.
  A build with `requires` runs that command first, and a failure marks the build as skipped instead of failed.
  This is for optional toolchains, such as a nightly Rust for AddressSanitizer.

The binaries speak three protocols, all defined on the Go side:

| Binary   | Protocol                                     | Reference                           |
| -------- | -------------------------------------------- | ----------------------------------- |
| driver   | driver line protocol, one answer per command | `dev/internal/protocol/driver.go`   |
| probe    | prints the probe report lines                | `dev/internal/conformance/proberef` |
| fuzzcase | runs a seed pack, prints the count           | `dev/internal/conformance/fuzzcase` |
| bench    | bench line protocol                          | `dev/internal/protocol/bench.go`    |

The existing drivers in `rust/tools/src/main.rs`, `zig/src/main.zig`, `ts/src/driver.ts`, `native/cpp/driver.cpp`, and `native/c/driver.c` are the models for a new one.
A binary can be a script: the TypeScript hosts are shell wrappers that run the sources under Node.

## The steps

The kit runs these steps, in this order, and records each outcome with its duration.

| Step             | Scope   | What it checks                                                                                                                                                                       |
| ---------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| generated        | repo    | `dev/cmd/generate -check`: every generated artifact is current.                                                                                                                      |
| corpus           | repo    | Builds the corpus, the stress rounds and the fuzz seed pack, and answers them with the Go engine.                                                                                    |
| build            | backend | The release build commands succeed and produce the named binaries.                                                                                                                   |
| probe            | backend | The probe output equals the Go probe report, line by line.                                                                                                                           |
| corpus           | backend | The driver answers the 87,415 commands of the fixed corpus exactly like the Go engine.                                                                                               |
| stress           | backend | The driver answers `-stress` extra random rounds of 500 patterns, from seed `-seed`.                                                                                                 |
| fuzz             | backend | The fuzzcase binary runs every seed of the pack and reports the count.                                                                                                               |
| checked/`name`/* | backend | Each checked build repeats build, probe, corpus and fuzz, on the quick and light corpus.                                                                                             |
| lean-data        | repo    | `lean/data/corpus.tsv` and `lean/data/probe.expected` equal the current corpus and probe report.                                                                                     |
| lean             | repo    | With `-lean`: `lake build`, then `vegocheck data/corpus.tsv` replays the corpus under the proofs, and `speccheck data/corpus.tsv` walks it under the model of the ERE specification. |

`-skip` leaves steps out, by name; a name applies to the release build and to every checked build.
The backends run concurrently, and the report keeps manifest order.
`-quick` shrinks every random block of the release corpus by ten, for a smoke run.
The checked builds always run the quick corpus in its light form.
The light corpus keeps every compile and contract command but leaves out the executions of `revera.HeavyPattern`, `((a*){250}){250}b`, whose capacity fallback costs tens of milliseconds per execution.
The Lean replay leaves those executions out as well, plus those of `((a*){4}){4}`, and the release corpus still runs all of them.
`-allow-skip` accepts skipped steps in the exit status; the verdict still names them.

The stress rounds are reproducible.
Round `i` uses seed `seed+i`, and the seed alone selects the flag set and the alphabet.
A later run with `-seed 120 -stress 30` therefore continues a first run with `-seed 100 -stress 20` without repeating a pattern.
`dev/internal/conformance/crosscheck -extra n` runs the same rounds from seed 100.

The Lean steps tie the backend to the proofs.
The theorems in `lean/Vego/Theorems.lean` replay the same corpus that the driver answered, so a backend that passes the corpus step agrees with the engine the proofs cover.
The same theorems check that corpus against the Lean model of the ERE specification, so a backend that passes the corpus step also answers every covered command as the specification requires.
The `lean-data` step fails when the corpus changed and the Lean data was not regenerated, which would silently split the two.

## The fuzz entry points

One input format drives every fuzz entry point, so one seed pack and one reproducer serve all backends.
`dev/internal/protocol/fuzz.go` defines it:

```
byte 0      compile flags, masked with 0x0f
byte 1      bits 0 and 1 are the exec flags; bit 4 selects the cs locale, else bit 5 selects tr
byte 2      n, the pattern length
n bytes     the pattern
1 byte      m, the replacement length
m bytes     the replacement
rest        the subject
```

The procedure is the same everywhere: select the locale, compile, exec with captures, replace, iterate three matches, compute the contract.
Every result is ignored; a crash, an assertion, or a sanitizer report is the signal.

| Backend | Entry point                                       | Coverage-guided run                                                    |
| ------- | ------------------------------------------------- | ---------------------------------------------------------------------- |
| go      | `FuzzEngine` in `go/fuzz_test.go`                 | `cd go && go test . -run '^$' -fuzz FuzzEngine`                        |
| rust    | `fuzz_one` in `rust/tools/src/fuzz.rs`            | `cd rust/fuzz && cargo +nightly fuzz run engine` (needs cargo-fuzz)    |
| zig     | `fuzzOne` in `zig/src/fuzz.zig`                   | `cd zig && zig build test --fuzz`                                      |
| ts      | `fuzzOne` in `ts/src/fuzz.ts`                     | none; the seed pack runner is the only form                            |
| cpp     | `LLVMFuzzerTestOneInput` in `native/cpp/fuzz.cpp` | `cd native/cpp && make libfuzzer && ./libfuzzer corpus/` (Linux clang) |
| c       | `LLVMFuzzerTestOneInput` in `native/c/fuzz.c`     | `cd native/c && make libfuzzer && ./libfuzzer corpus/` (Linux clang)   |

The Go entry point does more than the others: it also compares the Go engine with the reference engine on compile, exec and replace, so it is a differential fuzzer for the engine logic that every backend inherits.
The `fuzzcase` binaries are the deterministic form: they run a seed pack with no coverage feedback, which is what the kit needs for a bounded, repeatable verdict.
The seed pack is `tmp/conformance/fuzz-seeds.pack`, written by the corpus step from `revera.FuzzSeeds()`.

## The checked builds

| Backend | Build                            | What it catches                                                             |
| ------- | -------------------------------- | --------------------------------------------------------------------------- |
| rust    | `cargo build`                    | The runtime's `assert!` bounds checks, in the debug profile.                |
| rust    | `sh asan-build.sh`, nightly ASan | Out-of-bounds and use-after-free through raw pointers.                      |
| zig     | `zig build -p zig-out/debug`     | Every Zig safety check of Debug mode.                                       |
| ts      | `make check`, `tsc --noEmit`     | The type checker; the runtime checks of the engine are on in every build.   |
| cpp     | `make sanitize`                  | AddressSanitizer and UndefinedBehaviorSanitizer, fatal on the first report. |
| c       | `make sanitize`                  | AddressSanitizer and UndefinedBehaviorSanitizer, fatal on the first report. |

The Rust sanitizer build needs the nightly toolchain and is skipped without it, and the TypeScript type check needs the `typescript` package that `npm install` provides.
A skipped step keeps the verdict from being complete, so the exit status stays 1 unless `-allow-skip` is given.

## Adding a backend

1. Write the printer and the runtime, and add the generation step to `dev/cmd/generate`.
2. Write the driver, the probe, the fuzzcase runner and the bench binary, following the existing ones.
3. Add `backend.json` with the release build and at least one checked build.
4. Run `make conform CONFORM_FLAGS="-backend ../<dir>"`.
5. Run `make bench` and `make size` and record the figures in `docs/BENCHMARKS.md`.

The kit reports the first ten mismatched lines of a corpus run with the command, the expected answer and the answer received.
`cd dev && go run ./internal/conformance/godriver` answers the same protocol with the Go engine, so a single command can be replayed by hand.
`cd dev && go run ./internal/conformance/crosscheck -dump corpus.txt` writes the whole command stream for a driver under a debugger.
