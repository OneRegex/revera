# Revera for Zig

Revera is a POSIX ERE regex engine with leftmost-longest matching, and this directory is the Zig package `revera`.

`src/engine.zig` is generated from `revera.vego.json` at the repository root by `vegoc emit zig`.
Do not edit it.
`src/revera.zig` is the hand-written public API, and `src/vg.zig` is the small runtime that the Vego specification asks every target to supply.
The other hand-written source files support tests and development tools.

## Depend on it

Add the package to your project with `zig fetch`:

```sh
zig fetch --save https://github.com/oneregex/revera/releases/download/v0.1.0/revera-zig-0.1.0.tar.gz
```

Then wire the module in your `build.zig`:

```zig
const revera = b.dependency("revera", .{ .target = target, .release = true });
exe.root_module.addImport("revera", revera.module("revera"));
```

The package reads `release`, a boolean, and not `optimize`.
When `release` is true the engine builds in ReleaseSafe, and otherwise in Debug.
Both keep every bounds check.

## Use it

```zig
const std = @import("std");
const revera = @import("revera");

var re = try revera.Regex.compile(gpa, "([a-z]+)([0-9]*)", .{});
defer re.deinit();

var caps = (try re.captures("__abc12__")).?;
defer caps.deinit();
std.debug.print("{s}\n", .{caps.get(1).?.text()});
```

`src/revera.zig` is the whole public surface, and `build.zig` exports it as the module `revera`.

- `Regex.compile` takes an allocator, the pattern and an options struct.
  The Regex owns memory until `deinit`.
- A search takes a const pointer and keeps no state between calls.
  One Regex serves any number of threads, as long as the allocator it was compiled with is thread safe.
  Every search takes its scratch memory from that allocator.
- `find` and `captures` return an optional.
  A subject that does not match gives null, and only a real failure gives an error.
- `matches` and `captureMatches` return iterators with the usual `next` method.
- Engine failures use a plain error set, such as `error.InvalidPattern` and `error.OutOfCapacity`.
  Allocator exhaustion returns `error.OutOfMemory` instead of aborting.
  `Options.error_position` receives the byte offset in the pattern.
- `Locale.open(gpa, "cs", "")` selects a CLDR locale for bracket expressions.
  The default is POSIX.
- `re.contract(max_input)` reports what one search can cost, before it runs.

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`, are not exposed by this package.

## Layout

The package itself is the set of files listed in `.paths` of `build.zig.zon`: the API, the engine, the runtime, the data blob, the tests, and the two files that `zig build test` needs for the fuzz seed corpus.
The other files are the tools that the repository uses to verify the engine, and they do not ship with the package.

| File                    | Role                                                                                                          |
| ----------------------- | ------------------------------------------------------------------------------------------------------------- |
| `src/revera.zig`        | The public API. It embeds `data.bin` with `@embedFile`.                                                       |
| `src/engine.zig`        | The generated engine.                                                                                         |
| `src/vg.zig`            | The runtime: `Slice` and `Str` with Go slice semantics, the integer conversion helper, and string comparison. |
| `src/data.bin`          | The CLDR locale blob, embedded at build time.                                                                 |
| `src/revera_test.zig`   | The tests of the public API.                                                                                  |
| `src/host.zig`          | What the tools share: the locale loader and the token helpers of the line protocols.                          |
| `src/main.zig`          | The differential driver. It speaks the protocol of `dev/internal/protocol/driver.go`.                         |
| `src/probe_main.zig`    | The probe runner. It prints the lines of `dev/internal/conformance/proberef`.                                 |
| `src/bench_main.zig`    | The bench binary. It speaks the protocol of `dev/internal/protocol/bench.go`.                                 |
| `src/fuzz.zig`          | The fuzz entry point, with `dev/internal/protocol/fuzz.go` as the reference.                                  |
| `src/fuzzcase_main.zig` | The replayer for a pack of fuzz inputs.                                                                       |
| `src/probe_engine.zig`  | The generated probe engine.                                                                                   |

The runtime holds no state, so each thread can run its own engine instance.

Regenerate `src/engine.zig` and `src/probe_engine.zig` from the repository root:

```sh
make generate GENERATION_TARGETS=zig
```

## Build and verify

A plain `zig build` compiles nothing and installs nothing.
It only exposes the module.
The driver, probe, bench and fuzzcase tools have their own build file in `tools/`, outside the package, and it reaches the sources through a path dependency on the package:

```sh
zig build test
zig build --build-file tools/build.zig -Drelease -p zig-out
(cd ../dev && go run ./internal/conformance/crosscheck ../zig/zig-out/bin/driver)
(cd ../dev && go run ./internal/conformance/probecheck ../zig/zig-out/bin/probe)
(cd ../dev && go run ./cmd/conform -backend ../zig)
```

`zig build test` runs the API tests and the fuzz seed corpus.
`-Drelease` selects ReleaseSafe, and without it the tools build in Debug.
The runtime keeps its bounds asserts in every mode.
An out-of-range index aborts, which is the Go behavior the specification requires.

`probe` is the runner for the `vego/probe` package.
That package covers the subset constructs the engine never uses.

## Bench

`zig-out/bin/bench` times one engine operation per `B` command and reports its allocations.
`dev/cmd/bench` drives it with the shared cases, but it also answers by hand:

```sh
printf 'P\nB m match 100 3 0 28617c62292b 616261626162 2d\n' | zig-out/bin/bench
```

The answer is `B m 0 <bytes> <allocs> <ns> <ns> <ns>`.
The bytes and allocations count the requests the engine makes to its allocator during one operation.
An untimed pass measures them through a counting allocator that wraps the arena, and the timed passes use the plain arena.
Time comes from the `awake` clock of `std.Io`, which is monotonic.

## Fuzz

`src/fuzz.zig` exposes `fuzzOne`, which runs compile, exec, replace, iteration and the contract on one input.
Every target reads the same input layout, and the comment at the top of the file describes it.
The file also holds the test `engine fuzz`, which `zig build test` runs on a small seed corpus.
The same binary fuzzes with the built-in fuzzer:

```sh
zig build test --fuzz=10K
zig build test --fuzz
```

The first form runs a bounded number of inputs and prints a coverage report.
The second form runs until interrupted and serves a coverage web interface on a local port.
Both keep their corpus under `.zig-cache/f/`.
On macOS the fuzzer works with this Zig version, and only Windows and 32-bit targets are excluded.

`zig-out/bin/fuzzcase <packfile>` replays a pack of inputs through `fuzzOne`.
A pack is a sequence of records, each a little-endian `u32` length followed by that many bytes.
It prints `fuzzcase: <count> inputs` and exits 0, and a crash is the signal.
A missing or truncated pack gives a message on stderr and exit status 1.

## Checked build

Every safety check stays on in Debug mode, and the Debug binaries go to a separate prefix:

```sh
zig build --build-file tools/build.zig -p zig-out/debug
```

That produces `zig-out/debug/bin/driver`, `probe`, `bench` and `fuzzcase`, and leaves `zig-out/bin` alone.
`zig-out/` is ignored by git, so the extra prefix does not show in `git status`.

## Release archive

The Zig package manager wants `build.zig.zon` at the root of the archive, and this directory is not the root of the repository.
A release therefore attaches `revera-zig-0.1.0.tar.gz`, built from `zig/` with exactly the files selected by `.paths`, including `LICENSE` and `LICENSES/`, under one top-level directory.
`make dist` at the repository root builds this deterministic archive from committed `HEAD`.
It refuses tracked changes, mismatched Rust, Zig and native package versions, missing or stale license copies, and an undated release changelog.
Run `make licenses` before committing a release when the root license files change.
