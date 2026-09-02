# Revera for TypeScript

Revera is a POSIX.1-2024 extended regular expression engine.
This directory is the TypeScript instantiation and the source of the `revera` npm package.

The same engine exists in Go, Rust, Zig, C, C++ and TypeScript, generated from one Vego source and exercised by one cross-language conformance suite.
In addition, the Lean development gives Vego machine-checked semantics, and the repository's Lean README states its exact proof coverage.

The engine speaks the POSIX ERE language: leftmost-longest matching, no backreferences, no Perl escapes.
Therefore, it is the engine to reach for when a pattern must mean the same thing as it does in `regcomp()` and `regexec()`.

## Using it

```ts
import { Regex } from "revera";

const re = new Regex("([a-z]+)([0-9]*)");
const caps = re.captures("__abc12__");
console.log(caps?.get(1)?.text); // "abc"
```

`src/revera.ts` is the module entry point and the whole public surface.

- `Regex` compiles a pattern and searches.
  The constructor throws a `RegexError` when the pattern is invalid.
  A search keeps no state between calls, so one `Regex` serves any number of subjects.
- `test` answers yes or no.
  `find` and `captures` return a `Match` or a `Captures`, or `null` when nothing matches.
  A group that took no part in the match reads as `null` from `Captures.get`.
- `matches` and `captureMatches` are generators over the non-overlapping matches.
- `replaceAll` rewrites the subject the way `sed s///g` does, with `&` and `\1` through `\9` in the replacement, and takes an optional limit.
  `replaceAllWith` takes a function instead of a replacement text.
- `RegexError` carries a `kind`, one of the `<regex.h>` error names in kebab case, and a byte `offset` when the failure has one.
- The options object of the constructor carries `caseInsensitive`, `newlineSensitive`, `noCaptures`, `shortestMatch` and `locale`.
- `Locale.open("cs")` selects a CLDR locale for bracket expressions and case folding.
  The default is POSIX.
- `re.contract(maxInput)` reports what one search can cost, before it runs.
  Its figures are `bigint`, because the bounds reach 2^62.

Patterns and subjects are UTF-8.
A `string` is encoded before the engine sees it, and every offset the module reports is a byte offset into that encoding, not a UTF-16 index.
A `Uint8Array` subject is used as it is, so a subject that is not valid UTF-8 can be searched too.
`Match.text` decodes the matched bytes, and `Match.bytes` returns them as a view of the subject.

The execution flags of `regexec()`, `REG_NOTBOL` and `REG_NOTEOL`, are not exposed by this package.

## Numbers

JavaScript has one number type, a double, which is exact for integers up to 2^53.
Vego, the language the engine is written in, computes in 64-bit integers.
The printer maps the two like this:

| Vego type                     | TypeScript type | How                                                           |
| ----------------------------- | --------------- | ------------------------------------------------------------- |
| `int`, `int32`, `uint32`, ... | `number`        | The narrow types wrap with `\| 0`, `>>> 0` and `& 0xff`.      |
| `int64`, `uint64`             | `bigint`        | Exact at 64 bits, wrapped with `BigInt.asIntN` and `asUintN`. |

`int` is the index type, so it is everywhere: lengths, offsets, counters.
Its values are bounded by the size of a subject, and a subject longer than 2^53 bytes does not exist in practice.
Every 64-bit add, subtract, multiply and left shift on an `int` still goes through a check, and a result past 2^53 throws a `RangeError` instead of losing precision silently.
A constant may sit past that limit when a double holds it exactly, such as a power of two that serves as a sentinel; the engine has one.

`int64` carries the resource contracts, whose bounds saturate at 2^62, and `uint64` carries the memo hash and the capture masks.
Both map to `bigint`, so nothing in the contract or in the matcher is approximated.
The probe program, which exercises the corners of the language on purpose, reproduces the reference report bit for bit, including the wrap of `MinInt64 / -1`.

## Layout

```
| Path                           | Role                                                  |
| ------------------------------ | ----------------------------------------------------- |
| src/revera.ts                  | The public API. It reads data.bin at load time.       |
| src/engine.ts                  | The generated engine. Do not edit it.                 |
| src/probe_engine.ts            | The generated probe. Do not edit it.                  |
| src/vg.ts                      | The runtime the Vego specification asks for.          |
| src/data.bin                   | The CLDR locale blob.                                 |
| test/revera.test.ts            | Tests of the public API, under node --test.           |
| src/driver.ts                  | The differential driver.                              |
| src/probe_main.ts              | The probe runner.                                     |
| src/bench_main.ts              | The bench driver.                                     |
| src/fuzz.ts                    | The fuzz entry point.                                 |
| src/fuzzcase_main.ts           | The seed pack runner.                                 |
| src/host.ts                    | What the hosts share: blob, loader, hex, line reader. |
| driver, probe, bench, fuzzcase | Shell wrappers that run the hosts under Node.         |
| backend.json                   | How the conformance kit builds and runs this.         |
| Makefile                       | The syntax check, the type check and the tests.       |
```

`src/engine.ts` and `src/probe_engine.ts` come from `revera.vego.json` at the repository root.
The printer is `vegoc emit ts`, the package `vego/compiler/printer/ts`.

Regenerate them from the repository root:

```sh
make generate GENERATION_TARGETS=ts
```

The same thing runs as `cd ../dev && go run ./cmd/generate -target ts`.
The output is byte-exact and is not formatter clean by design, so never format the generated files.

The hosts import `src/engine.ts` and `src/vg.ts` directly and never touch `src/revera.ts`, so a driver failure points at the engine or the runtime and not at the API layer.

`src/vg.ts` is the runtime.
It supplies `Str`, which is a `Uint8Array` that nothing writes to, `Slice<T>`, a Go slice header over a typed array or a plain array, the buffer operations with the growth rule of the Vego specification, and the integer helpers.
A slice header never changes after construction, so sharing one is as safe as copying a Go slice header.
Structs become classes with a `clone` method, and the printer clones at every site where Go copies a struct value: a store from a place expression, and a return of a parameter, a field or an element.

The Vego specification describes an explicit memory context that every allocating function receives.
This target has none: the garbage collector owns every buffer, so the generated functions take no allocator parameter.
The runtime keeps one pair of counters for the bench host, and nothing else.

## Build and verify

The hosts and the tests run straight from the TypeScript sources.
Node 22.18 or later executes `.ts` files directly, so there is no build step.
The type checker is the one thing that `npm install` adds.

```sh
npm install
npm run check                 # tsc --noEmit
npm test                      # node --test test/
(cd ../dev && go run ./internal/conformance/crosscheck ../ts/driver)
(cd ../dev && go run ./internal/conformance/probecheck ../ts/probe)
```

`driver` speaks the line protocol that `dev/internal/protocol/driver.go` defines, and `crosscheck` runs the corpus through it.
`probe` prints the lines of the `vego/probe` package, which covers the subset constructs the engine never uses.
For comparison, `dev/internal/conformance/proberef` prints the reference lines, and `probecheck` compares the two outputs.

The one-command check is the conformance kit:

```sh
(cd ../dev && go run ./cmd/conform -backend ../ts)
```

It reads `backend.json`, runs the syntax check of the release build and the type check of the checked build, and runs the probe, the corpus and the fuzz seeds against each.
Every runtime check of the engine, bounds and integer range alike, is on in both builds; the checked build adds the type checker, and it is skipped when the `typescript` package is not installed.

The generated code runs about ten times slower than the native targets on the corpus.
Every element access carries a bounds check, every slice operation allocates a header, and the memo hash runs on `bigint`.
`docs/BENCHMARKS.md` records the figures.
