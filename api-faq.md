# API FAQ

Notes on standard-library behavior that differed from first expectations.

## Go

### regexp.CompilePOSIX newline handling

- Context: using Go's POSIX mode as a differential oracle for
  leftmost-longest whole-match selection.
- Expectation: `CompilePOSIX` implements POSIX matching, so dot matches
  newline and `^`/`$` bind to the subject boundaries.
- Reality: the POSIX flag set keeps RE2's newline conventions. Dot and
  negated classes exclude newline, and anchors match at every line
  boundary. `(?s)` cannot fix it: flag groups are not POSIX syntax and
  fail to compile.
- Resolution: the comparison uses newline-free subjects and avoids
  negated classes, so both conventions coincide.

### testing.AllocsPerRun under the race detector

- Context: asserting a zero-allocation steady state for the match path.
- Reality: race instrumentation adds allocations, so the assertion is
  only valid without `-race`. The test skips itself via a build-tagged
  `raceEnabled` flag.
