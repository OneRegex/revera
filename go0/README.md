# revera: POSIX.1-2024 ERE in Go

This module is a cleanroom Go implementation of
[`docs/POSIX-1-2024-ERE-SPECIFICATION.md`](../docs/POSIX-1-2024-ERE-SPECIFICATION.md).
It implements the full Issue 8 Extended Regular Expression language,
including the Issue 8 repetition modifier (`*?`, `+?`, `??`, `{m,n}?`)
and `REG_MINIMAL`.

## Packages

- `revera` is the regex engine and its regcomp-style API.
- `revera/locale` supplies `LC_CTYPE` and the ERE-relevant `LC_COLLATE`
  operations from embedded CLDR 48.2 data. It covers the POSIX locale and
  all 1,122 CLDR locales, with collation-type selection. It never reads
  the host locale database, so results are identical on every platform.
- `gen/genlocale` regenerates `locale/data.bin` from the C data file
  `src/rv_locale_data.inc` at the repository root.

## API

```go
loc := locale.POSIX()                  // or locale.Open("cs", "")
re, err := revera.Compile("(a|ab)(c|bcd)(d*)", loc, 0)
pmatch := make([]revera.Match, re.NumSub()+1)
matched, err := re.Exec("abcd", pmatch, 0)
```

The API mirrors `regcomp()`/`regexec()`:

- `Compile` flags: `ICase`, `Newline`, `NoSub`, `Minimal`.
  `REG_EXTENDED` is implicit; the module implements only EREs.
- `Exec` flags: `NotBOL`, `NotEOL`.
- `Match` holds half-open byte offsets; `{-1, -1}` marks a
  nonparticipating group. `Exec` fills `pmatch` exactly like `regexec`,
  and leaves it untouched under `NoSub` or when it is empty.
- Errors carry the POSIX code (`BadPat`, `EBrack`, `ECollate`, ...) and
  the pattern byte offset.
- A compiled `Regexp` is immutable and safe for concurrent `Exec` calls.

### Global operations

`MatchAll` walks every non-overlapping match, left to right, and calls
a callback with the filled `pmatch` slice. The callback returns false
to stop early. `ReplaceAll` rewrites every match with a sed-style
replacement text: `&` inserts the whole match, `\1` through `\9`
insert one group, and a backslash escapes the next character.
`ReplaceAllFunc` asks a callback for each replacement instead; the
returned text is inserted literally.

Each function takes a limit on the number of matches, before the
flags. A negative limit means no bound, like the `preg_replace`
limit. With `ReplaceAll` and `ReplaceAllFunc`, the subject past the
last counted match stays as it is.

```go
result, err := re.ReplaceAll("aabb xab", `\2\1`, -1, 0)
err = re.MatchAll(subject, -1, 0, func(pmatch []revera.Match) bool {
    fmt.Println(subject[pmatch[0].So:pmatch[0].Eo])
    return true
})
result, err = re.ReplaceAllFunc(subject, -1, 0, func(pmatch []revera.Match) string {
    return strings.ToUpper(subject[pmatch[0].So:pmatch[0].Eo])
})
```

All three follow the usual global-substitution rule: the next search
starts at the previous match end, and a null match there is skipped.
All three need offsets, so they refuse an expression compiled with
`NoSub`.

### Resource contracts

`CompileWithContract` compiles like `Compile` and also returns a
`Contract`: worst-case bounds on heap, stack, and abstract steps for one
`Exec` call on a subject of at most `maxInput` bytes. The figures come
per backend (the phase A matcher, the one-pass capture walk, the capture
solver), and the `HeapBytes`, `StackBytes`, and `Steps` methods combine
them for a whole call. An application can compare the figures against
its budget and refuse a pattern before it ever runs.

```go
re, c, err := revera.CompileWithContract(pattern, loc, 0, 1<<16)
if err == nil && (c.HeapBytes() > heapBudget || c.Steps() > stepBudget) {
    // refuse the expression
}
```

Heap figures count the explicit allocations the engine performs, with
fixed 64-bit field sizes, so they are identical on every platform.
Allocator rounding, object headers, and garbage collection are not
counted. Steps are abstract unit-cost operations, not time; they are
worst-case bounds, and ordinary subjects stay far below them.

## Input model

Patterns and subjects are UTF-8, length-delimited strings.

- NUL is an ordinary character. This input cannot reach a C `regexec()`,
  so it is a permitted extension. Dot still never matches NUL.
- Bytes that are not valid UTF-8 match nothing, and a match never starts
  or ends inside them.

## Engine

Compilation parses the ERE, checks it against the grammar, and lowers it
to a flat instruction program. Execution has two phases:

- Phase A scans the subject once and finds the selected match start and
  end. It runs all viable paths in lockstep, so it is linear in the
  subject for a fixed pattern. Its workspace is pooled; the match-only
  path performs zero allocations per call in steady state.
- Phase B runs only when capture offsets are requested. When compile-time
  analysis proves the pattern has at most one parse per span, a one-pass
  walk reads the group spans directly. Otherwise a memoized search
  computes the best parse of the selected span under the section 4.3
  selection order.

Shortest-preferring repetitions ride along as small counter vectors in
phase A, so `REG_MINIMAL` and the repetition modifier change selection
without a separate engine.

## Capacity

- `DupMax` is 255, the POSIX minimum for `RE_DUP_MAX`.
- Nested intervals multiply. When the expanded program passes an
  internal cap, compilation still succeeds. Execution then answers from
  the pattern's minimum match length: shorter subjects report no match,
  a nullable anchor-free pattern still answers existence queries, and
  only a subject that could really need the oversized program reports
  `ESpace`.
- Subjects must be shorter than 2 GiB; longer ones report `ESpace`.

## Conformance testing

- The spec section 16 examples run as unit tests.
- A reference matcher enumerates every parse and applies the selection
  rules literally. Randomized differential tests compare the engine with
  it across flags, locales, and multi-character collating elements.
- A second differential compares whole-match selection with Go's
  `regexp.CompilePOSIX` on long subjects, within the common subset.
- A third differential, gated on cgo and macOS, compares full capture
  vectors with the host `regcomp()` and `regexec()`.
- The locale blob is generated from the C tables at the repository root.
  A lookup dump over 19 locale selections came out bit-identical to the
  C implementation.

The chosen outcomes for undefined and unspecified constructs are listed
in [`NOTES.md`](NOTES.md).
