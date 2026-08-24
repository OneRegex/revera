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
- Phase B runs only when capture offsets are requested. It computes the
  best parse of the selected span under the section 4.3 selection order
  with a memoized search.

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
- Subjects up to 2 GiB are supported; larger ones report `ESpace`.

## Conformance testing

- The spec section 16 examples run as unit tests.
- A reference matcher enumerates every parse and applies the selection
  rules literally. Randomized differential tests compare the engine with
  it across flags, locales, and multi-character collating elements.
- A second differential compares whole-match selection with Go's
  `regexp.CompilePOSIX` on long subjects, within the common subset.
- The locale tables are validated bit-for-bit against the C
  implementation in this repository.

The chosen outcomes for undefined and unspecified constructs are listed
in [`NOTES.md`](NOTES.md).
