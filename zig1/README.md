# zig1: the revera engine in Zig

This directory is the Zig instantiation of the Vego engine from `go1/`.

`src/engine.zig` is generated from `go1/revera.vego.json` by `go1/cmd/json2zig`.
Do not edit it.
The hand-written files are the public API and the minimal runtime that the Vego specification asks every target to supply.

## Using it

```zig
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

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`, are not on this surface.
A caller who needs them imports `engine.zig`.

## Layout

- `src/revera.zig` is the public API.
  It embeds `data.bin` with `@embedFile`.
- `src/engine.zig` is the generated engine.
  Regenerate it with:

  ```sh
  cd ../go1
  go run ./cmd/revera generate -target zig
  ```

- `src/vg.zig` is the runtime.
  It supplies the `Slice` and `Str` value types with Go slice semantics, the integer conversion helper, and string comparison.
  It holds no state, so each thread can run its own engine instance.
- `src/host.zig` holds what the hand-written hosts share: the embedded locale blob with its loader, and the token helpers of the line protocols.
- `src/main.zig` is the differential driver.
  It speaks the line protocol that `go1/revera/driver_host.go` defines.
- `src/bench_main.zig` is the bench binary.
  It speaks the line protocol that `go1/revera/bench_host.go` defines.
- `src/fuzz.zig` is the fuzz entry point, and `src/fuzzcase_main.zig` replays a seed pack through it.
- `src/revera_test.zig` covers the public API.
- `src/data.bin` is the CLDR locale blob, embedded at build time.

## Build and verify

```sh
zig build test
zig build -Drelease
cd ../go1 && go run ./cmd/crosscheck ../zig1/zig-out/bin/driver
cd ../go1 && go run ./cmd/probecheck ../zig1/zig-out/bin/probe
```

The build also produces `probe`, the runner for the `go1/probe` package.
That package covers the subset constructs the engine never uses.

The build defaults to ReleaseSafe, and the runtime keeps its bounds asserts in every mode.
An out-of-range index aborts, which is the Go behavior the specification requires.

## Bench

`zig-out/bin/bench` times one engine operation per `B` command and reports its allocations.
`go1/cmd/bench` drives it with the shared cases, but it also answers by hand:

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
zig build -p zig-out/debug
```

That produces `zig-out/debug/bin/driver`, `probe`, `bench` and `fuzzcase`, and leaves `zig-out/bin` alone.
`zig-out/` is ignored by git, so the extra prefix does not show in `git status`.
