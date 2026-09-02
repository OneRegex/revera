# TRE divergences from POSIX extended regular expressions

Status: source and behavior audit performed 2026-08-24.

## Baselines and scope

The specification baseline is [POSIX.1-2024, Issue 8](https://pubs.opengroup.org/onlinepubs/9799919799/), the latest published POSIX edition at the time of this audit.
The parts used here are:

- [XBD Chapter 9, Regular Expressions](https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap09.html), especially Sections 9.1, 9.2, 9.4, and 9.5
- [XBD `<regex.h>`](https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/regex.h.html)
- [XSH `regcomp()`, `regexec()`, `regerror()`, and `regfree()`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/regcomp.html)
- [XBD Rationale A.9](https://pubs.opengroup.org/onlinepubs/9799919799/xrat/V4_xbd_chap01.html#tag_21_09)

The implementation baseline is `third_party/tre` commit `71bfcaf0af3994384987c6c2679ed7d078ffe189`, which identifies itself as TRE 0.9.0.
TRE's own README still names IEEE Std 1003.1-2001 / Issue 6 as its conformance target ([`README.md`, lines 101-116](../third_party/tre/README.md)).

This document covers:

- ERE syntax and matching semantics.
- bracket-expression rules shared by BREs and EREs.
- the parts of `<regex.h>` and the `regcomp()` family that directly control ERE compilation, execution, and reported offsets.
- TRE extensions that change the accepted ERE language or matching result.

It does not compare BRE syntax, the `agrep` command-line interface, language bindings, performance, or internal algorithm design.
Approximate matching is included only because TRE exposes it through ERE syntax and flags.

The terms below are deliberate:

- **Conformance gap**: TRE does not implement a behavior or interface that Issue 8 requires.
- **Extension**: TRE gives meaning to syntax or an option outside the POSIX ERE language.
  Extensions in areas where POSIX says behavior is undefined are permitted and are not conformance failures.
- **Implementation choice**: POSIX explicitly leaves the behavior undefined or unspecified and TRE selects one allowed behavior.

## Summary

The audited tree has six concrete conformance gaps:

| Area | POSIX.1-2024 requirement | TRE behavior |
| --- | --- | --- |
| Collating symbols | A valid `[.collating-element.]` inside a bracket expression denotes that collating element. | TRE returns `REG_ECOLLATE` for every `[.` item, including the single-character `[[.a.]]`. |
| Equivalence classes | A valid `[=collating-element=]` denotes its primary equivalence class; if the element has no class, it is treated as a collating symbol. | TRE returns `REG_ECOLLATE` for every `[=` item, including `[[=a=]]`. |
| Minimal-match compilation flag | `<regex.h>` defines `REG_MINIMAL`, which reverses the default greediness of ERE duplication symbols and the meaning of the `?` repetition modifier. | TRE does not define or test `REG_MINIMAL`. It implements the same behavior under the nonstandard name `REG_UNGREEDY`. |
| Match-offset type in the standalone ABI | `regoff_t` is a signed integer type able to hold the largest value representable by either `ptrdiff_t` or `ssize_t`. | TRE declares `regoff_t` as `int` when it does not use the system regex ABI. On the audited LP64 build it was 32 bits while `ptrdiff_t` and `ssize_t` were 64 bits. |
| `REG_NOSUB` output argument | When an expression was compiled with `REG_NOSUB`, `regexec()` must ignore `pmatch`. | On a successful match TRE writes `{-1,-1}` to every one of the `nmatch` elements, even though `REG_NOSUB` was set. |
| Case-insensitive non-matching lists | Under XBD 4.1's closure, a string that matches case-sensitively must keep matching after case-counterpart substitution. Case-insensitive `[^a]` must therefore match `a`, because `A` matches case-sensitively. | TRE case-expands the positive list and then inverts it, so case-insensitive `[^a]` matches neither `a` nor `A`. |

TRE's [`TODO`, lines 12-15](../third_party/tre/TODO) acknowledges the first two gaps as missing features.
The README understates the first one, because the parser rejects single-character collating symbols as well as multi-character ones.

Issue 8 standardized the `*?`, `+?`, `??`, and `{m,n}?` repetition syntax that TRE already implements.
That syntax is therefore no longer a TRE extension.
The incompatibility is the missing standardized flag name and handling, not the repetition-modifier syntax.

## Required behavior that TRE does not implement

### 1. Collating symbols

POSIX bracket expressions allow a collating element to be written between `[.` and `.]` delimiters, for example `[[.a.]]`.
A valid explicit collating symbol is part of the portable bracket-expression grammar.
Multi-character collating elements are locale-dependent, but the single-character elements of the current locale are not optional.

TRE's bracket parser unconditionally returns `REG_ECOLLATE` as soon as an item begins with `[.` ([`lib/tre-parse.c`, lines 296-301](../third_party/tre/lib/tre-parse.c)).
It never parses the element or asks the locale whether it is valid.

Observed in the isolated default build:

| Pattern | Locale-independent subject | `tre_regcomp()` result |
| --- | --- | --- |
| `[[.a.]]` | `a` | `REG_ECOLLATE` |
| `[[.ch.]]` | `ch` | `REG_ECOLLATE` before locale validity is considered |

Consequences include:

- valid single-character collating symbols cannot be used as bracket members.
- a collating symbol cannot be a range endpoint, not even in the portable form that makes `-` the first endpoint.
- locale-defined multi-character collating symbols cannot be expressed.
- case-insensitive matching of explicit multi-character collating elements cannot be implemented.

POSIX leaves it unspecified whether an ordinary matching or non-matching list recognizes multi-character collating elements implicitly.
TRE's failure to do that is not itself a gap.
The gap is rejection of the explicit `[.element.]` syntax.

### 2. Equivalence class expressions

POSIX defines `[=element=]` inside a bracket expression as the set of collating elements in the same primary equivalence class.
If the named element belongs to no equivalence class, the expression must behave as a collating symbol for that element.

TRE's parser unconditionally returns `REG_ECOLLATE` for an item beginning with `[=` at the same parser branch cited above.
For example, `[[=a=]]` fails compilation even in the POSIX locale.
There it must at least behave like the collating symbol for `a`.

This is a separate gap from multi-character collation: even a one-character equivalence expression cannot compile.

### 3. `REG_MINIMAL`

Issue 8 added both the repetition modifier `?` and the `REG_MINIMAL` compilation flag.
With ordinary ERE compilation, a modifier changes only its duplication from leftmost-longest to leftmost-shortest.
With `REG_MINIMAL`, repetitions are shortest by default and the modifier requests longest matching for the repetition it follows.

TRE implements those semantics but exposes them as `REG_UNGREEDY`:

- the flag is defined at [`include/tre/tre.h`, lines 135-140](../third_party/tre/include/tre/tre.h).
- the parser takes its default `minimal` value from `REG_UNGREEDY`, and inverts it for a following `?`.
  See [`lib/tre-parse.c`, lines 623-653 and 835-863](../third_party/tre/lib/tre-parse.c).
- the same inversion is implemented for `*`, `+`, and `?` ([`lib/tre-parse.c`, lines 1127-1167](../third_party/tre/lib/tre-parse.c)).

The audited header contains no definition of `REG_MINIMAL`, and the implementation contains no test for it.
Therefore:

- an Issue 8 source program that uses `REG_MINIMAL` does not compile against the standalone header of TRE.
- when system-ABI mode includes a host header that defines `REG_MINIMAL`, TRE still does not interpret that bit on purpose.
  It works only when the host value happens to equal the independently assigned `REG_UNGREEDY` value of TRE.
- replacing `REG_MINIMAL` with `REG_UNGREEDY` produces the required exact-matching behavior.
  For example, both `.*?` with ordinary flags and `.*` with `REG_UNGREEDY` matched the empty prefix of `abcd`.
  `.*?` with `REG_UNGREEDY` matched all four characters.

The syntax and API documentation of TRE still describe minimal repetition as an extension over Issue 6.
See [`doc/tre-syntax.html`, lines 96-156](../third_party/tre/doc/tre-syntax.html) and [`doc/tre-api.html`, lines 131-134](../third_party/tre/doc/tre-api.html).
That description is out of date for Issue 8.

TRE states that minimal repetitions are not supported by its approximate matcher.
Approximate matching is not part of POSIX, so this limitation is not an additional POSIX gap for ordinary ERE matching.

### 4. `regoff_t` in TRE's standalone ABI

Issue 8 requires `regoff_t` to hold the largest value representable by either `ptrdiff_t` or `ssize_t`.
TRE's non-system-ABI declarations use:

```c
typedef int regoff_t;
```

at [`include/tre/tre.h`, lines 92-106](../third_party/tre/include/tre/tre.h).
The implementation also sets `TRE_MAX_STRING` to `INT_MAX` ([`lib/tre-internal.h`, lines 28-30](../third_party/tre/lib/tre-internal.h)).

In the audited default macOS build:

```text
sizeof(regoff_t)  = 4
sizeof(ptrdiff_t) = 8
sizeof(ssize_t)   = 8
```

The type therefore cannot represent every offset that the POSIX type contract requires on that ABI.
A build against a usable system regex ABI imports the `regoff_t` of the system, and can avoid this gap.
`--enable-system-abi` is optional, and [`configure.ac`, lines 121-127](../third_party/tre/configure.ac) disables it by default.

### 5. `REG_NOSUB` does not make `pmatch` unused

POSIX requires `regexec()` to ignore its `pmatch` argument when either `nmatch` is zero or the expression was compiled with `REG_NOSUB`.
In particular, an application need not supply an array merely because `nmatch` is nonzero when `REG_NOSUB` applies.

TRE suppresses the real match offsets under `REG_NOSUB`.
It then fills every one of the `nmatch` output elements with `{-1,-1}`.
The first branch of `tre_fill_pmatch()` skips submatch construction, after which its final loop writes the unused entries ([`lib/regexec.c`, lines 54-114](../third_party/tre/lib/regexec.c)).
`tre_match()` calls this function after every successful match without overriding `nmatch` when `REG_NOSUB` applies ([`lib/regexec.c`, lines 139-201](../third_party/tre/lib/regexec.c)).

A focused probe set one `regmatch_t` to `{123,456}` and compiled `a` with `REG_EXTENDED | REG_NOSUB`.
It then called `tre_regexec()` with `nmatch == 1`.
Matching succeeded and the structure changed to `{-1,-1}`.
This is an observable write through an argument that the standard requires the implementation to ignore.
A pointer that would be safe only when ignored can therefore fault.

### 6. Case-insensitive non-matching lists

XBD 4.1 defines case-insensitive matching as a closure.
Take a string that matches the RE case-sensitively.
The same string, with any character replaced by its `toupper` or `tolower` counterpart, shall also match case-insensitively.
Applied to a non-matching list, the closure runs over the case-sensitive result of the inverse.
`A` matches `[^a]` case-sensitively, and `a` is `A` with one counterpart replacement, so case-insensitive `[^a]` is required to match `a`.
The "simple algorithm" of rationale A.4.1 produces the same result.
It checks each subject character in both cases, and matches when either case matches.

TRE instead expands case before inverting.
For ordinary members and ranges, the bracket parser adds opposite-case counterpart items to the positive list, at [`lib/tre-parse.c`, lines 380-414](../third_party/tre/lib/tre-parse.c).
It then builds the negated union from that expanded list, at [`lib/tre-parse.c`, lines 441-457](../third_party/tre/lib/tre-parse.c).
Negated character classes take a separate path with the same effect.
The matcher rejects a character when either of its cases belongs to the class, at [`lib/tre-match-utils.h`, lines 204-215](../third_party/tre/lib/tre-match-utils.h).

Observed in the isolated default build, POSIX locale, `REG_EXTENDED | REG_ICASE`:

| Pattern | Subject | Issue 8 closure requires | TRE result |
| --- | --- | --- | --- |
| `[^a]` | `a` | match | no match |
| `[^a]` | `A` | match | no match |
| `[^[:lower:]]` | `a` | match | no match |
| `[^a-z]` | `m` | match | no match |

Two control probes ran in the same build.
Case-sensitive `[^a]` matches `A`, and the positive-direction closure works, because `[a]` and `a` with `REG_ICASE` both match `A`.

One caution about scope.
TRE shares this behavior with the other implementations probed in this audit series.
The macOS system `regcomp()` and `grep -i` behave the same way.
The closure reading follows the literal Issue 8 text, as recorded in [`POSIX-1-2024-ERE-SPECIFICATION.md`, Section 10.2](POSIX-1-2024-ERE-SPECIFICATION.md).
It is a gap against that text, not a TRE-specific defect relative to common practice.

## ERE language extensions

These constructs are not in the POSIX ERE grammar.
Where the POSIX text assigns undefined behavior to the underlying spelling, accepting it as an extension is explicitly permitted.

| TRE construct | TRE meaning | POSIX status |
| --- | --- | --- |
| `\0` through `\9` | Single-digit back-reference syntax; it selects TRE's backtracking matcher. `\0` is accepted although undocumented, and a forward reference is accepted if that capture exists later in the ERE. | Back-references exist only in the POSIX BRE grammar. An escaped ordinary digit in an ERE has undefined meaning. |
| `\a`, `\e`, `\f`, `\n`, `\r`, `\t` | Control-character escapes. | Escaping an ERE ordinary character is undefined. A literal newline remains subject to `REG_NEWLINE`. |
| `\d`, `\D`, `\s`, `\S`, `\w`, `\W` | Macros for digit, space, and word bracket expressions and their negations. TRE defines word as alphanumeric or underscore. | Not POSIX ERE syntax; these are meanings assigned to otherwise undefined escapes. |
| `\<`, `\>`, `\b`, `\B` | Beginning/end-of-word, word-boundary, and non-word-boundary zero-width assertions. | Not POSIX ERE assertions; POSIX has only `^` and `$`. |
| `\xHH`, `\x{H...}` | Numeric character escapes. The short form consumes zero through two hexadecimal digits, and the braced form accepts zero through eight; an empty digit sequence has value zero. | Not POSIX ERE syntax. |
| `\Q ... \E` | Temporarily quote pattern text as literal. | Not POSIX ERE syntax. |
| `(?#comment)` | Ignored comment. | Not in the POSIX grouping grammar. |
| `(?:ERE)` | Non-capturing group. | POSIX groups always count as parenthesized subexpressions. |
| `(?flags)ERE` and `(?flags:ERE)` | Change `i`, `n`, `r`, or `U` options for the rest of a containing group or for one group. A `-` turns options off. | POSIX compilation flags apply to the compiled expression as a whole; inline option groups are not defined. |
| `{,n}` | Zero through `n` repetitions. | POSIX intervals require the first count. A left brace not in a valid interval has undefined behavior. |
| `{,}` | Zero or more repetitions. | Same undefined area; equivalent in language to `*`. |
| `{+...-...#...~..., ...}` settings | Per-subexpression insertion, deletion, substitution, total-error, and cost controls for approximate matching. | Approximate matching and this interval-like syntax are outside POSIX. |

The escape macros are enumerated in [`lib/tre-parse.c`, lines 53-63](../third_party/tre/lib/tre-parse.c).
The parser implementations for quoting, assertions, numeric escapes, and ERE back-references are at [`lib/tre-parse.c`, lines 1383-1530](../third_party/tre/lib/tre-parse.c).
Approximate and omitted-bound parsing is at [`lib/tre-parse.c`, lines 615-863](../third_party/tre/lib/tre-parse.c).
Inline groups are handled at [`lib/tre-parse.c`, lines 1217-1376](../third_party/tre/lib/tre-parse.c).

Although TRE's syntax manual limits back-references to `\1` through `\9`, the parser actually recognizes any single digit.
Thus `\0` is also accepted and matched the empty string in the focused probes.
A sequence such as `\12` is parsed as back-reference `\1` followed by literal `2`.
These are further choices in an area where POSIX ERE behavior is undefined, not additional conformance gaps.

## Choices for undefined or unspecified POSIX input

The following differences are observable, but POSIX does not prescribe a result for them.
They must not be described as conformance bugs.

### Empty expressions and alternation branches

The POSIX ERE grammar requires one or more branches and one or more expressions per branch.
An empty ERE, `()`, and a `|` with a missing adjacent branch do not conform to that grammar.
Their results are therefore undefined.

TRE treats each missing expression as an empty expression that matches the empty string.
The audited build accepted all of these:

```text
empty pattern
()
|a
a|
a||b
```

This behavior comes from the parser's explicit empty-node paths ([`lib/tre-parse.c`, lines 1357-1376 and 1620-1651](../third_party/tre/lib/tre-parse.c)).
It is an allowed extension, not the POSIX meaning of an empty ERE.

### Escaped ordinary characters

Outside bracket expressions, POSIX defines escapes for ERE special characters plus `]` and `}`.
The interpretation of a backslash before another ordinary character is undefined.
TRE either applies one of its extensions above or drops the backslash and matches the following character literally.
Thus `\q` matches `q`.

### Left braces that do not form intervals

POSIX makes an unescaped `{` that is not part of a valid interval undefined.
TRE usually tries to parse it as a bound and returns an error.
The audited patterns `{x` and `a{x` returned `REG_BADBR`.

This contradicts TRE's syntax manual, which says that `{` followed by a non-digit is ordinary ([`doc/tre-syntax.html`, lines 368-371](../third_party/tre/doc/tre-syntax.html)).
The source and executable behavior are authoritative for this audit.

### Adjacent duplication symbols

POSIX says multiple adjacent duplication symbols have undefined behavior, apart from the standardized trailing `?` repetition modifier.
TRE makes several different choices:

- `a{2}{2}` is accepted and behaves as a nested repeat, matching four `a` characters.
- `a*{2}` is accepted as a bound applied to `a*`.
- further postfix operators can be applied after a minimal modifier, so `a*??` is accepted.
- combinations such as `a**`, `a*+`, `a++`, and a bound followed by `*` or `+` return `REG_BADRPT`.

The repeated postfix loop and its selective error checks are at [`lib/tre-parse.c`, lines 1127-1180](../third_party/tre/lib/tre-parse.c) and [`lib/tre-parse.c`, lines 835-863](../third_party/tre/lib/tre-parse.c).

### Ranges and multi-character elements not explicitly named

TRE forms ranges from numeric character values and rejects a descending pair ([`lib/tre-parse.c`, lines 284-301](../third_party/tre/lib/tre-parse.c)).
Issue 8 requires the POSIX locale's collation sequence, but deliberately leaves range behavior unspecified in other locales.
Consequently:

- numeric ordering in a non-POSIX locale is an allowed choice, not a current conformance gap.
- rejecting a range whose represented set would be empty is one of the allowed outcomes.
- not recognizing an implicit multi-character collating element from an ordinary bracket list is unspecified and is not an additional gap.

Explicit collating symbols and equivalence classes remain required, as described above.

## Other nonstandard flags and APIs

TRE is namespaced as a library rather than installed as the system POSIX regex provider:

- the primary header is `<tre/tre.h>` and the exported functions are `tre_regcomp()`, `tre_regexec()`, `tre_regerror()`, and `tre_regfree()`.
- `<tre/regex.h>` provides source-level compatibility by macro-aliasing the unprefixed names ([`include/tre/regex.h`, lines 13-37](../third_party/tre/include/tre/regex.h)).
- the standalone prototypes do not reproduce the Issue 8 `restrict` qualifiers.
  This changes neither the C ABI nor the matching result.
  It is a source-header difference from the specified `<regex.h>` declarations.

TRE also exposes the following options outside POSIX:

| Interface | Effect |
| --- | --- |
| `REG_BASIC` | Explicit name for the zero-valued default BRE mode. |
| `REG_LITERAL` / `REG_NOSPEC` | Treat the entire pattern literally. |
| `REG_RIGHT_ASSOC` | Use right-associative concatenation; this can change capture allocation while leaving the whole match unchanged. |
| `REG_UNGREEDY` | TRE's name for the behavior Issue 8 standardizes as `REG_MINIMAL`. |
| `REG_USEBYTES` | Treat input units as raw bytes. |
| `REG_APPROX_MATCHER` | Force the approximate matcher. |
| `REG_BACKTRACKING_MATCHER` | Force the backtracking matcher. |
| `REG_BADMAX` | More specific extension error for a repetition count above `RE_DUP_MAX`; in system-ABI mode it aliases `REG_BADBR`. |
| `REG_OK` | Name the zero success result. |

The definitions are in [`include/tre/tre.h`, lines 54-90 and 109-161](../third_party/tre/include/tre/tre.h).
TRE also adds the `reg_errcode_t` typedef used by its implementation.
POSIX permits additional names that begin with `REG_`, so their existence is not a conformance failure.
Passing `REG_RIGHT_ASSOC`, `REG_LITERAL`, or an approximate-matching option deliberately requests behavior outside the POSIX ERE contract.

Additional APIs support length-delimited strings, embedded NUL bytes, wide-character strings, raw byte vectors, approximate results, and caller-provided streaming sources.
[`include/tre/tre.h`, lines 178-301](../third_party/tre/include/tre/tre.h) declares them.
POSIX `regcomp()` and `regexec()` accept NUL-terminated byte strings and cannot express embedded NUL bytes.
The added APIs extend rather than contradict the standard interfaces.

## Confirmed Issue 8 alignments

The following areas were checked because older descriptions of POSIX EREs commonly differ.
They are not divergences in this tree:

| Area | Result |
| --- | --- |
| Leftmost match and submatches | TRE's exact matcher is designed to select the leftmost-longest whole match and recursively ordered submatches. No contrary behavior was found in the audited source or focused probes. |
| Minimal repetition syntax | `*?`, `+?`, `??`, and `{m,n}?` implement Issue 8 leftmost-shortest repetition, measured in characters rather than merely iteration count. |
| Anchors anywhere in an ERE | TRE parses unescaped `^` and `$` as anchors everywhere. Thus `a^` and `$a` compile but cannot match `a`, as Issue 8 requires. |
| `REG_NEWLINE`, `REG_NOTBOL`, `REG_NOTEOL` | Dot and non-matching-list newline exclusion and the anchor exceptions around newline agree with `regcomp()` requirements. |
| Escaped `]` and `}` outside brackets | TRE's general escape path makes `\]` and `\}` literal, as Issue 8 requires. |
| Ordinary backslash inside brackets | TRE treats backslash as an ordinary bracket member, consistent with the POSIX bracket-expression rules. |
| Standard character classes | TRE delegates class lookup and membership to `wctype()` / `iswctype()` when available and supplies the standard class set in its fallback. Unknown names return `REG_ECTYPE`. |
| Repetition limits | TRE sets `RE_DUP_MAX` to 255 and accepts the three required interval forms through that value. |
| Pattern length | TRE caps a pattern at 65,536 units. POSIX only requires support for every RE of 256 bytes or fewer, so this larger implementation limit is permitted. |
| `regfree()` and `errno` | A focused probe preserved a sentinel `errno` value across `tre_regfree()`, consistent with the Issue 8 requirement. |

## Verification record

The build came from an isolated copy.
It kept the default wide-character, multibyte and approximate-matching support, and it disabled system ABI support.
The standard TRE regression executables `retest`, `wretest`, and `test-str-source` passed.
The complete `make check` run stopped while the long `test-limits` test was still executing.
This audit therefore does not claim a complete green upstream suite.

Focused probes directly against the built static library established:

```text
REG_MINIMAL=undefined
sizeof(regoff_t)=4 sizeof(ptrdiff_t)=8 sizeof(ssize_t)=8
REG_NOSUB with nmatch=1 changed pmatch from {123,456} to {-1,-1}
[[.a.]]       -> REG_ECOLLATE
[[=a=]]       -> REG_ECOLLATE
.*? / abcd    -> match [0,0)
.* + REG_UNGREEDY / abcd -> match [0,0)
(a)\1 / aa    -> match [0,2)
a{,2} / aaa   -> match [0,2)
(?i:a) / A    -> match [0,1)
empty ERE / x -> match [0,0)
a{2}{2} / aaaa -> match [0,4)
```

The case-insensitive non-matching-list probes were run later on the same revision from a fresh isolated copy.
That copy needed `glibtoolize --copy` and `autoreconf -fi` before `./configure --disable-shared --disable-agrep`.
The resulting feature set matched the original audit build: wide-character, multibyte and approximate matching on, system ABI off.
The probe program linked `lib/.libs/libtre.a`, called `setlocale(LC_ALL, "POSIX")`, and used `tre_regcomp()` with `REG_EXTENDED | REG_ICASE` unless noted:

```text
[^a] / a            -> no match
[^a] / A            -> no match
[^a] / b            -> match
[^a] / a  (no icase) -> no match
[^a] / A  (no icase) -> match
[a] / A             -> match
a / A               -> match
[^[:lower:]] / a    -> no match
[^a-z] / m          -> no match
```

Source inspection covered every ERE production in XBD 9.5.3 and the shared bracket-expression grammar in XBD 9.5.2.
It also covered the general and ERE semantic rules in XBD 9.1, 9.2 and 9.4, and the ERE-relevant `<regex.h>` and `regcomp()` contracts.
Within that scope, the lists above are complete for this revision.
The sixth gap arrived after the original inspection pass.
It appeared when the `REG_ICASE` sections of the specification moved to the literal XBD 4.1 closure.
