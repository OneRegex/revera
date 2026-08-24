# Mistakes log

## 2026-08-24, ERE specification review

- I wrote that `{CHARCLASS_NAME_MAX}` is runtime-increasable "via
  `sysconf()`". The limits are runtime-increasable, but Issue 8 defines a
  `sysconf()` variable only for `{RE_DUP_MAX}` (`_SC_RE_DUP_MAX`). I
  generalized from the `<limits.h>` section preamble without checking the
  `sysconf()` variable table. Lesson: verify each named API hook, not the
  category it sits in.
- I recommended that the specification document `regexec()` errors such as
  `REG_ESPACE` as standard-allowed. The normative RETURN VALUE text only
  specifies `REG_NOMATCH`; the "or an error" wording sits in the
  DESCRIPTION. The right advice was to record the tension, not to add
  outcomes. Lesson: separate the standard's letter from common practice in
  recommendations, exactly as I demanded of the reviewed document.
- I called the `REG_ICASE` `[^a]` behavior "universal practice" after
  testing only macOS libc and macOS grep. Lesson: claim only what was
  tested, and record the probe.
