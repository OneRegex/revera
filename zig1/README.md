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
  go run ./cmd/json2zig -o ../zig1/src/engine.zig revera.vego.json
  go run ./cmd/json2zig -o ../zig1/src/probe_engine.zig probe.vego.json
  ```

- `src/vg.zig` is the runtime.
  It supplies the `Slice` and `Str` value types with Go slice semantics, the integer conversion helper, and string comparison.
  It holds no state, so each thread can run its own engine instance.
- `src/main.zig` is the differential driver.
  It speaks the line protocol that `go1/revera/driver_host.go` defines.
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
