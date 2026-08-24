# Repository Guidelines

## Project Structure & Module Organization

This repository currently contains specification and reference material rather
than a root-level implementation. `docs/POSIX-1-2024-ERE-SPECIFICATION.md` is
the clean-room POSIX ERE contract. `docs/TRE-POSIX-ERE-DIVERGENCES.md` records
source-backed differences between that contract and the pinned TRE tree.
`ref/tre`, `ref/re2`, and `ref/minrx` are independent upstream checkouts used
as evidence and design references. Keep project documentation in `docs/`; do
not mix project changes into `ref/` unless deliberately updating a reference
revision.

## Build, Test, and Development Commands

There is no top-level build or test command yet. For documentation work, use:

- `rg -n 'TODO|FIXME|TBD' docs` to find unfinished claims.
- `rg -n '../ref/' docs` to review links into reference source.
- `git -C ref/tre rev-parse HEAD` (and the equivalent for `re2` or `minrx`) to
  confirm the exact revision cited by an audit.

When a claim depends on executable behavior, test an isolated copy of the
relevant reference. Examples are `make -C ref/re2 test` and
`make -C ref/minrx tryit`. TRE uses Autotools; run `./configure && make check`
inside a disposable copy because it creates generated files and can be slow.

## Coding Style & Naming Conventions

Write Markdown with sentence-case headings, short paragraphs, and lines wrapped
near 80 characters. Put syntax, flags, commands, and paths in backticks. Use
precise conformance terms: keep required behavior, invalid input, undefined
behavior, unspecified choices, and extensions distinct. Name new audit files
descriptively in uppercase kebab case, following
`TRE-POSIX-ERE-DIVERGENCES.md`. Cite authoritative standards and exact local
source paths for technical claims.

## Testing Guidelines

Treat each behavioral assertion as needing reproducible evidence. Record the
reference revision, configuration, locale, command, and observed result. Add
focused probes for boundary cases, then run the applicable upstream suite.
Do not claim a complete pass when a suite was skipped, stopped, or timed out.

## Commit & Pull Request Guidelines

No top-level Git history is available in this snapshot, so use concise,
imperative commit subjects such as `Document TRE REG_NOSUB divergence`.
Keep commits limited to one claim or reference update. Pull requests should
explain the contract being changed, link the authoritative source or issue,
identify affected reference revisions, and list exact validation commands and
results. Include screenshots only for rendered-document formatting changes.
