# reference: the POSIX.1-2024 ERE reference engine

This package is a clean-room Go implementation of [`docs/POSIX-1-2024-ERE-SPECIFICATION.md`](../../../docs/POSIX-1-2024-ERE-SPECIFICATION.md).
It is the reference engine, not a supported Go implementation.

By contrast, the supported Go implementation is the `go` module at the repository root, and the differential tests compare that engine with this one.

It implements the full Issue 8 Extended Regular Expression language.
Specifically, it includes the Issue 8 repetition modifier, written `*?`, `+?`, `??` and `{m,n}?`, and `REG_MINIMAL`.

## Packages

- `reference`, imported as `github.com/oneregex/revera/dev/internal/reference`, is the engine and its regcomp-style API.
- `reference/locale`, imported as `github.com/oneregex/revera/dev/internal/reference/locale`, supplies `LC_CTYPE` and the ERE-relevant `LC_COLLATE` operations from embedded CLDR 48.2 data.
  It covers the POSIX locale and all 1,122 CLDR locales, with collation-type selection.
  It never reads the host locale database, so the results are identical on every platform.
- `reference/libcre`, imported as `github.com/oneregex/revera/dev/internal/reference/libcre`, is the cgo wrapper around the host `regcomp()` and `regexec()` for the libc differential test.
- `dev/internal/genlocale` rebuilds this package's `locale/data.bin` from the repository-root file `locale/rv_locale_data.inc`.
  Run it as `cd dev && go run ./internal/genlocale`; it writes all seven checked-in copies of `data.bin`.

## API

```go
loc := locale.POSIX()                  // or locale.Open("cs", "")
re, err := reference.Compile("(a|ab)(c|bcd)(d*)", loc, 0)
pmatch := make([]reference.Match, re.NumSub()+1)
matched, err := re.Exec("abcd", pmatch, 0)
```

The API mirrors `regcomp()` and `regexec()`:

- `Compile` flags are `ICase`, `Newline`, `NoSub` and `Minimal`.
  `REG_EXTENDED` is implicit, because the package implements only EREs.
- `Exec` flags are `NotBOL` and `NotEOL`.
- `Match` holds half-open byte offsets, and `{-1, -1}` marks a nonparticipating group.
  `Exec` fills `pmatch` exactly like `regexec`.
  It leaves `pmatch` untouched under `NoSub`, and when the slice is empty.
- An error carries the POSIX code, such as `BadPat`, `EBrack` or `ECollate`, and an optional byte offset.
  Compilation offsets refer to the pattern, while replacement syntax offsets refer to the replacement text.
- A compiled `Regexp` never changes, so concurrent `Exec` calls are safe.

### Global operations

`MatchAll` walks every non-overlapping match, left to right, and calls a callback with the filled `pmatch` slice.
The callback returns false to stop early.

`ReplaceAll` rewrites every match with a sed-style replacement text.
`&` inserts the whole match, `\1` through `\9` insert one group, and a backslash escapes the next character.

By contrast, `ReplaceAllFunc` asks a callback for each replacement and inserts the returned text literally.

Each function takes a match limit before the flags.
As with the `preg_replace` limit, a negative value means no bound.

For `ReplaceAll` and `ReplaceAllFunc`, the subject past the last counted match stays unchanged.

```go
result, err := re.ReplaceAll("aabb xab", `\2\1`, -1, 0)
err = re.MatchAll(subject, -1, 0, func(pmatch []reference.Match) bool {
    fmt.Println(subject[pmatch[0].So:pmatch[0].Eo])
    return true
})
result, err = re.ReplaceAllFunc(subject, -1, 0, func(pmatch []reference.Match) string {
    return strings.ToUpper(subject[pmatch[0].So:pmatch[0].Eo])
})
```

All three follow the usual global-substitution rule.
The next search starts at the previous match end, and it skips a null match there.

Because all three need offsets, an expression compiled with `NoSub` reports `ENoSub`.

### Resource contracts

`CompileWithContract` compiles like `Compile` and also returns a `Contract`.

The resulting contract bounds explicit heap allocation requests and abstract steps for one `Exec` call on a subject of at most `maxInput` bytes, and estimates stack use from the deepest call chain.
The contract stores figures per backend, while the `HeapBytes`, `StackBytes` and `Steps` methods combine them for a whole call.

This structure lets an application compare the figures against its budget and refuse a pattern before it ever runs.

```go
re, c, err := reference.CompileWithContract(pattern, loc, 0, 1<<16)
if err == nil && (c.HeapBytes() > heapBudget || c.Steps() > stepBudget) {
    // refuse the expression
}
```

Heap figures count the explicit allocations that the engine performs, using fixed 64-bit field sizes.
Because those field sizes are fixed, the figures are identical on every platform.

However, they leave out allocator rounding, object headers and garbage collection.

Steps are abstract unit-cost operations, not time.
The reported step counts are worst-case bounds, although ordinary subjects stay far below them.

## Input model

Patterns and subjects are UTF-8, length-delimited strings.

- NUL is an ordinary character.
  Such input cannot reach a C `regexec()`, so it is a permitted extension.
  Dot still never matches NUL.
- Bytes that are not valid UTF-8 match nothing, and a match never starts or ends inside them.

## Engine

Compilation parses the ERE, checks it against the grammar, and lowers it to a flat instruction program.
Execution has two phases:

- Phase A scans the subject once and finds the selected match start and end.
  It runs all viable paths in lockstep, so it is linear in the subject for a fixed pattern.
  Its workspace is pooled, and the match-only path performs no allocation per call in steady state.
- Phase B runs only when the caller asks for capture offsets.
  A one-pass walk reads the group spans directly when compile-time analysis proves that each span has at most one parse.
  A memoized search otherwise computes the best parse of the selected span, under the section 4.3 selection order.

Shortest-preferring repetitions ride along as small counter vectors in phase A.
`REG_MINIMAL` and the repetition modifier therefore change selection without a separate engine.

## Capacity

- `DupMax` is 255, the POSIX minimum for `RE_DUP_MAX`.
- Nested intervals multiply.
  Compilation still succeeds when the expanded program passes an internal cap.
  Execution then answers from the minimum match length of the pattern.
  A shorter subject reports no match, and a nullable anchor-free pattern still answers existence questions.
  Only a subject that could really need the oversized program reports `ESpace`.
- A subject must be shorter than 2 GiB, and a longer one reports `ESpace`.

## Conformance testing

- The examples of specification section 16 run as unit tests.
- A reference matcher enumerates every parse and applies the selection rules literally.
  Randomized differential tests compare the engine with it across flags, locales and multi-character collating elements.
- A second differential compares whole-match selection with Go's `regexp.CompilePOSIX`, on long subjects, inside the common subset.
- A third differential, gated on cgo and macOS, compares full capture vectors with the host `regcomp()` and `regexec()`.
- The locale blob is generated from the C tables in `locale/` at the repository root.
  The Go engine's white-box locale test replays class and case-conversion answers generated by this reference package.

[`NOTES.md`](NOTES.md) lists the chosen outcomes for undefined and unspecified constructs.

## License

The reference code is licensed under the repository's [MIT license](../../../LICENSE).
Its embedded locale data is covered by the [Unicode License v3](../../../LICENSES/Unicode-3.0.txt).
