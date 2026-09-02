# Benchmarks

This document describes the cross-language benchmarks, the profiling workflow, and the figures they gave on the reference machine.
The numbers change with hardware and toolchains.
The method does not, so a rerun on another machine is comparable within itself.

## What is measured

One list of cases drives every engine.
`dev/internal/protocol/bench.go` defines the cases and the bench protocol, in the same way that `dev/internal/protocol/driver.go` defines the differential protocol.
Each target ships a `bench` binary that speaks the protocol with the generated engine, and `dev/cmd/bench` runs all of them over the same commands.
The Go engine runs in-process, and `-reference` adds the reference engine as a column.

The cases fall into four groups:

| Group   | What one operation is                        | Cases |
| ------- | -------------------------------------------- | ----- |
| compile | `Compile` of the pattern                     | 10    |
| match   | `Exec` with captures over the subject        | 15    |
| hard    | `Exec` on a pattern that stresses the engine | 10    |
| replace | `ReplaceAll` over the subject                | 4     |

The subjects are deterministic: a fixed pseudo-English text of about 1,000 bytes, plus short constructed strings.
The difficult patterns are the shapes that hurt a POSIX engine: nested stars, counted repetitions, ambiguous groups, many captures, five `.*` groups, the RE2 classic `[a-q][^u-z]{13}x`, and the capacity fallback of `((a*){250}){250}b`.

Each case runs a fixed number of iterations, chosen so one repetition takes a few milliseconds, five repetitions by default.
The Go sessions run a garbage collection before each repetition, as `testing.B` does, so garbage from the earlier passes does not land inside a timed one.
The tables report the fastest repetition; `-tsv` also records the slowest, so the noise of a run is visible.
An untimed pass counts allocations: the bytes and the number of requests that the operation makes to its allocator.
In Go these are heap allocations; in Rust, Zig, C++, and C11 they are arena requests, counted by the runtime.
A match operation allocates its match buffer, as every public API does.
The four generated targets report the same counts and, up to struct padding, the same bytes, which is expected, since they run the same program through the same growth rule, and it is a useful cross-check of the counters.

The contract column of the allocation table is `ContractHeapBytes` of the pattern for the length of the subject.
It is a bound, not a prediction, and the table shows how far the bound sits above the measured figure.

Compile in the targets resets the pattern arena before each iteration, and match and replace reset the scratch arena, which is the discipline of the drivers and of the public APIs.
Zig resets with `retain_capacity`, so its arena keeps its pages between iterations; Rust, C++, and C11 free every block.
The bench binaries call the generated engine directly and leave the hand-written wrappers out, so the figures compare the generated code, not the API layers.

## Running

```sh
make bench                          # builds the release binaries, prints the tables, writes tmp/bench-results.tsv
make bench BENCH_FLAGS="-reference" # adds the reference engine
make size                           # generated code size per target
make profile                        # CPU and allocation profiles of the Go engine
cd dev && go run ./cmd/bench -only hard/ -reps 3 -backend ../zig
```

`-only` takes a prefix of `group/name`, `-scale` multiplies the iteration counts, and `-backend` limits the run to some manifests.
The Go benchmarks also exist as `BenchmarkEngine` under `go test`, which is what `make profile` runs:

```sh
cd go && go test . -run '^$' -bench BenchmarkEngine -benchmem -cpuprofile ../tmp/cpu.pprof -memprofile ../tmp/mem.pprof -o ../tmp/revera.test
go tool pprof -top ../tmp/revera.test ../tmp/cpu.pprof
go tool pprof -sample_index=alloc_space -top ../tmp/revera.test ../tmp/mem.pprof
```

The Go profile is the profile of the engine source of truth.
An optimization goes into the Vego source, so the Go profile is where the decision is made, and the cross-language bench is where the effect is confirmed on every target.
For a target-native profile, `perf record` on Linux and Instruments on macOS work on the `bench` binaries, which take the same commands on stdin.

## Code size

`make size` reports three figures per engine.
The source figure is the generated engine source of the target, and the Vego source for Go.
The code figure is the machine code of the engine functions in the release driver, summed from the symbol table with `debug/macho` or `debug/elf`; the `engine_symbols` expression of `backend.json` selects the symbols, and for Go the host files are subtracted.
The text figure is the whole executable section of the driver, which includes the runtime and the standard library of the language.

Reference machine, 2026-08-31:

| engine | source bytes | source lines | code bytes | functions | driver text bytes |
| ------ | ------------ | ------------ | ---------- | --------- | ----------------- |
| go     | 144554       | 5212         | 78944      | 117       | 800860            |
| cpp    | 182579       | 5213         | 76436      | 215       | 84812             |
| rust   | 202520       | 5572         | 85600      | 101       | 328392            |
| zig    | 170335       | 5259         | 58652      | 72        | 376628            |

The engine is between 59 KB and 86 KB of machine code in every language.
The C++ figure counts 215 functions because every template instantiation of the runtime gets its own symbol; Rust and Zig inline most of them.
Several symbols can share one address, such as a function and its cold path label; they count once.
`revera.vego.json`, the artifact the printers read, is 1.3 MB.

## Figures

Reference machine: Apple M5 Max, macOS, Go 1.27.0, Apple clang 21, rustc 1.98.0, zig 0.17.0-dev.1936.
The run is `make bench BENCH_FLAGS="-reference"` on an idle machine, 2026-08-31.

Compile time, ns per Compile:

| case           |      go | reference |    cpp |   rust |    zig |
| -------------- | ------: | --------: | -----: | -----: | -----: |
| literal        |  726 ns |    596 ns | 392 ns | 490 ns | 392 ns |
| groups         |  845 ns |    634 ns | 473 ns | 504 ns | 409 ns |
| alternation    |  2.5 us |    2.1 us | 1.4 us | 1.5 us | 1.1 us |
| classes        |  843 ns |    692 ns | 635 ns | 577 ns | 454 ns |
| words          | 10.3 us |    9.6 us | 6.1 us | 7.6 us | 5.3 us |
| counted        |  622 ns |    584 ns | 496 ns | 551 ns | 392 ns |
| counted-255    |  4.7 us |    4.5 us | 3.6 us | 4.4 us | 3.1 us |
| nested-counted |  1.3 us |    1.2 us | 1.2 us | 1.3 us | 934 ns |
| icase-utf8     |  970 ns |    736 ns | 615 ns | 610 ns | 472 ns |
| cs-collating   |  860 ns |    636 ns | 568 ns | 578 ns | 444 ns |

Match time, ns per Exec with captures:

| case         |       go | reference |      cpp |     rust |      zig |
| ------------ | -------: | --------: | -------: | -------: | -------: |
| literal-hit  |   3.3 us |    3.1 us |   3.4 us |   3.8 us |   3.5 us |
| literal-miss |   3.2 us |    3.0 us |   3.2 us |   3.6 us |   3.4 us |
| groups-short |   1.5 us |    1.6 us |   1.2 us |   1.3 us |   1.2 us |
| groups-long  |  30.8 us |   31.1 us |  31.4 us |  35.2 us |  35.5 us |
| nosub-long   |  27.6 us |   27.6 us |  28.7 us |  32.5 us |  32.8 us |
| words        | 212.5 us |  178.4 us | 225.7 us | 254.8 us | 270.1 us |
| classes      |  13.6 us |   13.1 us |  14.3 us |  15.9 us |  15.4 us |
| anchored     |   2.7 us |    2.5 us |   2.9 us |   3.3 us |   3.2 us |
| dot-star     |  23.8 us |   21.5 us |  24.5 us |  29.0 us |  28.2 us |
| icase        |   3.6 us |    3.4 us |   3.5 us |   4.1 us |   3.8 us |
| newline      |   459 ns |    365 ns |   496 ns |   520 ns |   461 ns |
| minimal      |   3.0 us |    2.7 us |   3.1 us |   3.7 us |   3.7 us |
| utf8         |   304 ns |    204 ns |   325 ns |   357 ns |   312 ns |
| cs-collating |   4.3 us |    3.3 us |   3.3 us |   3.6 us |   3.4 us |
| tr-icase     |   303 ns |    224 ns |   363 ns |   382 ns |   346 ns |

Difficult patterns, ns per Exec with captures:

| case              |       go | reference |      cpp |     rust |      zig |
| ----------------- | -------: | --------: | -------: | -------: | -------: |
| nested-star       |   1.2 us |    1.0 us |   1.3 us |   1.5 us |   1.5 us |
| double-plus       |   1.4 us |    1.2 us |   1.5 us |   1.7 us |   1.7 us |
| nested-counted    | 174.0 us |  197.3 us | 139.9 us | 167.4 us | 185.5 us |
| five-dot-stars    |  1.34 ms |  810.1 us | 849.3 us | 930.8 us |  1.05 ms |
| counted-255       | 223.7 us |  194.8 us | 242.3 us | 287.9 us | 275.2 us |
| re2-classic       |  37.6 us |   39.9 us |  39.6 us |  41.5 us |  42.6 us |
| ambiguous-groups  | 438.0 us |  327.4 us | 348.8 us | 370.7 us | 369.9 us |
| many-groups       |   6.7 us |    6.3 us |   7.0 us |   8.3 us |   8.0 us |
| empty-loops       |   1.0 us |    831 ns |   1.1 us |   1.2 us |   1.2 us |
| capacity-fallback | 25.84 ms |  21.82 ms | 28.08 ms | 32.20 ms | 34.38 ms |

Replacement, ns per ReplaceAll:

| case          |       go | reference |      cpp |     rust |      zig |
| ------------- | -------: | --------: | -------: | -------: | -------: |
| literal       |   2.9 us |    2.0 us |   3.2 us |   3.4 us |   2.8 us |
| groups        | 124.9 us |  132.6 us | 105.3 us | 115.1 us | 115.1 us |
| empty-matches | 116.1 us |   45.1 us | 127.0 us | 127.9 us |  84.7 us |
| no-match      |   806 ns |    528 ns |   836 ns |   887 ns |   815 ns |

Allocation per operation, bytes and requests; the generated targets share one column because they report the same figures:

| case                   | go B/op | go allocs | reference B/op | reference allocs | targets B/op | targets allocs |   contract B |
| ---------------------- | ------: | --------: | -------------: | ---------------: | -----------: | -------------: | -----------: |
| compile/literal        |    4232 |        19 |           2944 |               26 |         2223 |             15 |              |
| compile/groups         |    4536 |        27 |           3128 |               35 |         3245 |             22 |              |
| compile/alternation    |   19857 |        58 |          10504 |               93 |        14614 |             54 |              |
| compile/classes        |    4920 |        29 |           3104 |               35 |         3120 |             26 |              |
| compile/words          |   86113 |       253 |          47832 |              416 |        65500 |            243 |              |
| compile/counted        |    3368 |        25 |           3208 |               28 |         3270 |             25 |              |
| compile/counted-255    |   35000 |       270 |          35360 |              272 |        28168 |            268 |              |
| compile/nested-counted |    7288 |        55 |           5792 |               61 |         8161 |             56 |              |
| compile/icase-utf8     |    4520 |        36 |           3104 |               38 |         2678 |             30 |              |
| compile/cs-collating   |    4352 |        27 |           3024 |               35 |         3398 |             24 |              |
| match/literal-hit      |     424 |         9 |              1 |                0 |          471 |             10 |  14000019379 |
| match/groups-short     |    7984 |        37 |            235 |                5 |         7737 |             30 |      1520457 |
| match/groups-long      |   21024 |        47 |            468 |                5 |        20733 |             40 | 418408594569 |
| match/nosub-long       |     656 |        14 |             70 |                1 |          553 |             10 |          969 |
| match/words            |    3656 |        22 |            201 |                3 |         3600 |             20 |  14800026428 |
| match/dot-star         |     384 |        11 |              3 |                0 |          403 |             10 | 412408289603 |
| match/cs-collating     |   20600 |        78 |            183 |                3 |        20077 |             57 |      1373993 |
| hard/nested-counted    | 1051146 |        93 |           9823 |                6 |       700761 |             78 |     23132809 |
| hard/five-dot-stars    | 9306675 |       134 |         369102 |               59 |      5581203 |            109 |   1216962883 |
| hard/counted-255       |   10624 |        27 |            271 |                0 |        10592 |             24 | 114000606276 |
| hard/ambiguous-groups  | 1717210 |       111 |          48704 |                8 |      1724421 |            104 |   2752451173 |
| hard/capacity-fallback | 8386352 |        82 |        4196016 |               45 |      4746602 |             50 |  35817171482 |
| replace/groups         |  478715 |      1575 |          12453 |              164 |       470587 |           1333 |              |
| replace/empty-matches  |  376561 |      9066 |           5792 |                6 |       361623 |           8039 |              |
| replace/no-match       |     456 |        10 |            145 |                3 |          516 |             11 |              |

The targets column shows the C++ figure for the compile cases.
Rust and Zig report a few percent less there, because they reorder struct fields while C++ keeps the declaration order, so a C++ AST node carries more padding.
The match and replace figures are identical in the three targets.
The full table with every column is in `tmp/bench-results.tsv` after a run.

## What the figures say

The four languages are within 30 percent of each other on almost every case, which is the expected outcome for one program printed four times.
Zig compiles fastest and C++ matches fastest; Rust is the slowest of the three on matching by 10 to 20 percent, with the release profile that keeps `debug-assertions` on.
The Go engine keeps up with the native targets on matching and is 20 to 40 percent slower on compilation, where its allocations go through the garbage collector.

The reference engine and the Go engine run the same algorithm, so their difference isolates one design decision.
The reference engine pools its workspace and its capture solver with `sync.Pool`; the Go engine allocates both fresh per `Exec`, because Vego has no pools and a compiled `Regexp` must stay read-only.
The allocation table shows it: zero requests per `Exec` on most reference engine matches, nine to eleven on the Go engine.
The cost is visible only where many small `Exec` calls chain, such as `replace/empty-matches`, where every empty match costs a workspace: 45 us for the reference engine against 116 us for the Go engine.
On single matches the difference is a few percent.

The contract bounds sit far above the measurements, by four to seven orders of magnitude on the phase A cases.
The bound must hold for every subject of the given length, and the solver bound grows with the number of ways the pattern can split a subject.
`match/nosub-long` shows the other end: 969 bytes bound against 553 measured, because a `FlagNoSub` pattern never reaches phase B.

The three generated targets make the same number of requests on every case, and the same bytes outside the padding of compiled nodes.
A difference there would mean a printer or a runtime diverges from the growth rule the specification fixes.

## Profile findings

The CPU profile of `BenchmarkEngine` on the Go engine puts 26 percent of the samples in the phase A executor: `paRun`, `paClosure`, `paStore`, `paConsume` and `paArrive`.
The largest single symbols after those are the Go runtime's scheduler and garbage collector, which are the cost of the 93 GB the benchmark run allocates in total.
The allocation profile explains where those bytes come from:

| site                     | share of bytes | what it is                                              |
| ------------------------ | -------------: | ------------------------------------------------------- |
| `addNode`                |            33% | the parser's node arena, grown by append                |
| `addInstr`               |            17% | the program builder, grown by append                    |
| `memoGrow`               |            16% | the memo tables of the capture solver, doubling from 64 |
| `prepare`                |            11% | the per-Exec workspace of phase A                       |
| `newTree` and `kidAlloc` |            14% | the parse trees of the capture solver                   |

By count, `prepare` is 35 percent of the objects and the append growth of the `active` arrays in `paStore` another 15 percent.

One experiment followed from the count figures, and it is recorded here because its result decides what to try next.
Giving every `active` array its full capacity up front removed the growth chain: the requests per `Exec` fell from 20 to 14 on `match/words` and from 24 to 14 on `hard/counted-255`, in every target.
The time did not move on any case, within the 2 percent noise of the run, and `replace/empty-matches` got 5 to 10 percent slower on the arena targets, because a tiny program now allocated two arrays that it used to skip.
The change was reverted.
A first attempt at the same experiment shared one block among the ring slots and was rejected by the Vego checker: a slice field takes a fresh buffer, a move, or a truncation of itself, never a view into another buffer.

The lesson is that the number of allocation requests is not where the time goes, in any of the four runtimes.
Bytes matter for Go, through the collector, and the phase A loop matters everywhere.
The next candidates, in the order the profile suggests, are:

1. The phase A inner loop for wide alternations: `match/words` runs a 20-way alternation at 220 ns per byte, ten times the cost of a literal.
2. The memo tables and parse trees of the capture solver: `hard/five-dot-stars` allocates 5.6 MB per `Exec` for a 60-byte subject, and `hard/capacity-fallback` 4.7 MB.
3. The parser and program arenas: a six-byte literal compiles through 15 to 19 allocations and 2 to 4 KB, most of it append growth from an empty slice.

Each candidate should start from `make profile`, land as a change to the Vego source, and be confirmed with `make bench` on every target before and after, with the two builds in one run, as the experiment above did.
