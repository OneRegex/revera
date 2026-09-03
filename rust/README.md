# Revera for Rust

Revera is a POSIX.1-2024 extended regular expression engine for UTF-8 strings.
It is useful when you need POSIX matching semantics, including leftmost-longest matching, locale-aware bracket expressions, and consistent behavior across Rust, Go, Zig, C, C++, and TypeScript.

[API documentation](https://docs.rs/revera) | [Source repository](https://github.com/oneregex/revera)

## Installation

Add Revera to your `Cargo.toml`:

```toml
[dependencies]
revera = "0.1"
```

The crate has no Cargo dependencies or feature flags.
Its CLDR locale data is embedded, so applications do not need a data file at runtime.

## Quick start

Compile a `Regex` once and reuse it for as many subjects as you need:

```rust
use revera::Regex;

fn main() -> revera::Result<()> {
    let re = Regex::new(r"([[:alpha:]]+)-([[:digit:]]+)")?;

    let caps = re.captures("item-42")?.expect("the example must match");
    assert_eq!(&caps[0], "item-42");
    assert_eq!(&caps[1], "item");
    assert_eq!(&caps[2], "42");

    Ok(())
}
```

`Regex::new` uses the POSIX locale and the default matching options.

Use `RegexBuilder` when you need case-insensitive matching, newline-sensitive anchors, shortest-preferring repetition, a different locale, or a yes-or-no-only expression.

## Pattern syntax and matching rules

Revera accepts POSIX ERE syntax, including alternation (`a|b`), groups (`(...)`), bracket expressions, anchors, and the `*`, `+`, `?`, and `{m,n}` repetition forms.
Interval bounds may be no greater than `revera::DUP_MAX`.

POSIX.1-2024 shortest-preferring repetition forms such as `*?`, `+?`, `??`, and `{m,n}?` are also supported.

Patterns and subjects are Rust `str` values, so both are valid UTF-8.
`Match::start`, `Match::end`, and `Match::range` return byte offsets into the subject, as Rust string slicing expects.

Search methods distinguish a missing match from an engine error:

- `is_match` returns `Result<bool>`.
- `find` returns `Result<Option<Match>>`.
- `captures` returns `Result<Option<Captures>>`.
- `find_iter` and `captures_iter` are lazy iterators whose items are `Result` values.

The iterators return non-overlapping matches from left to right.
An iterator must still handle errors that occur after iteration begins:

```rust
use revera::Regex;

fn main() -> revera::Result<()> {
    let re = Regex::new(r"[[:digit:]]+")?;
    let found = re
        .find_iter("12 apples, 30 pears")
        .map(|result| result.map(|matched| matched.as_str()))
        .collect::<revera::Result<Vec<_>>>()?;

    assert_eq!(found, ["12", "30"]);
    Ok(())
}
```

Group 0 is the whole match.

`Captures::get` returns `None` when a group did not participate or the index does not exist.

Indexing with `captures[i]` is convenient for a group known to participate, but it panics for an absent group.

`Captures::len` counts group 0, and `Captures::iter` visits every group in order.
`Regex::captures_len` reports the same group count without running a search.

```rust
use revera::Regex;

fn main() -> revera::Result<()> {
    let re = Regex::new(r"(cat)|(dog)")?;
    let caps = re.captures("cat")?.expect("the example must match");

    assert_eq!(caps.get(1).map(|m| m.as_str()), Some("cat"));
    assert_eq!(caps.get(2), None);
    Ok(())
}
```

`Match` and `Captures` borrow the subject string and cannot outlive it.

The temporary workspace for a search is released before the call returns, and the compiled `Regex` retains no result from earlier searches.

## Replacing matches

`replace_all` and `replacen` use replacement syntax similar to `sed`.

`&` inserts the whole match, and `\1` through `\9` insert capturing groups.

A backslash escapes the next replacement character, so `\&` inserts a literal ampersand and `\\` inserts a literal backslash.

Rust raw strings make these replacements easier to read.

```rust
use revera::Regex;

fn main() -> revera::Result<()> {
    let re = Regex::new(r"([[:alpha:]]+)-([[:digit:]]+)")?;

    let out = re.replace_all("id-7 and item-42", r"\1[#\2]")?;
    assert_eq!(out, "id[#7] and item[#42]");

    let first = re.replacen("id-7 and item-42", 1, "<&>")?;
    assert_eq!(first, "<id-7> and item-42");

    Ok(())
}
```

Use `replace_all_with` when the replacement needs Rust code:

```rust
use revera::Regex;

fn main() -> revera::Result<()> {
    let re = Regex::new(r"[[:alpha:]]+")?;
    let out = re.replace_all_with("one 2 three", |caps| caps[0].to_uppercase())?;

    assert_eq!(out, "ONE 2 THREE");
    Ok(())
}
```

## Compile options

`RegexBuilder` provides these options:

- `case_insensitive(true)` matches upper and lower case alike.
  Case folding follows the selected locale.
- `newline_sensitive(true)` gives `^` and `$` line-oriented behavior.
  It also prevents dot and negated bracket expressions from matching a newline.
- `shortest_match(true)` makes each repetition shortest-preferring by default.
  A shortest-preference modifier in the pattern reverses that choice for one repetition.
- `no_captures(true)` avoids recording match offsets when only a yes-or-no answer is needed.
  `is_match` remains available, while methods that need match spans report `ErrorKind::NoCaptures`.
- `locale(locale)` selects the locale used for character classes, case folding, collating elements, and equivalence classes.

```rust
use revera::RegexBuilder;

fn main() -> revera::Result<()> {
    let re = RegexBuilder::new(r"^hello[[:space:]]+world$")
        .case_insensitive(true)
        .newline_sensitive(true)
        .build()?;

    assert!(re.is_match("HELLO WORLD")?);
    Ok(())
}
```

`Regex` is `Send` and `Sync`.

A compiled expression keeps no per-search state, so it can be shared and searched concurrently.

## Locales

The default is the POSIX locale, also called the C locale.

`Locale::open` selects one of the embedded CLDR locales and returns `None` for an unknown locale name or collation type.

Pass an empty collation type to use the locale's standard collation.

```rust
use revera::{Locale, RegexBuilder};

fn main() -> revera::Result<()> {
    let czech = Locale::open("cs", "").expect("the embedded data includes cs");
    let re = RegexBuilder::new("[[.ch.]]")
        .locale(czech)
        .build()?;

    assert!(re.is_match("ch")?);
    Ok(())
}
```

`Locale::names()` lists the locale names in the embedded data.

`Locale::posix()` returns the default locale explicitly.

Most applications do not need the underlying bytes, but `embedded_locale_data()` exposes the exact locale blob compiled into the crate.

## Errors

Compilation, searching, iteration, and replacement are fallible.
An ordinary non-match is not an error: it is `Ok(false)` or `Ok(None)`, depending on the method.

`Error` implements `std::error::Error` and exposes structured information:

- `kind()` returns an `ErrorKind`, such as `Pattern`, `CharacterClass`, `Range`, `Capacity`, or `NoCaptures`.
- `offset()` returns an optional byte offset.
  Compilation offsets point into the pattern, while replacement escape and backreference offsets point into the replacement string.

```rust
use revera::{ErrorKind, Regex};

fn main() {
    let error = Regex::new("a(").expect_err("the pattern is invalid");

    assert_eq!(error.kind(), ErrorKind::Pattern);
    assert_eq!(error.offset(), Some(2));
    assert_eq!(error.to_string(), "invalid regular expression at byte 2");
}
```

`ErrorKind::Capacity` means the requested work exceeded an engine capacity limit.

Do not treat every `Err` as invalid syntax, and do not discard iterator errors with `filter_map` unless that is an intentional policy.

## Resource contracts

`Regex::contract(max_input)` reports the resource cost of one search for subjects up to `max_input` bytes.

The top-level `heap_bytes` and `steps` values are bounds, while `stack_bytes` is an estimate.

The `matcher`, `one_pass`, and `solver` fields provide the backend breakdown.

```rust
use revera::Regex;

fn main() -> revera::Result<()> {
    const MAX_SUBJECT_BYTES: usize = 16 * 1024;
    const HEAP_BUDGET_BYTES: u64 = 8 * 1024 * 1024;

    let re = Regex::new(r"([[:alpha:]]+)([[:digit:]]*)")?;
    let contract = re.contract(MAX_SUBJECT_BYTES);

    let accepted = contract.max_input == MAX_SUBJECT_BYTES
        && contract.heap_bytes <= HEAP_BUDGET_BYTES;
    if !accepted {
        println!("pattern rejected by the application's resource policy");
        return Ok(());
    }

    assert!(re.is_match("item42")?);
    Ok(())
}
```

Every contract figure saturates at `1 << 62` when the computed value is too large to be useful.

The returned `max_input` is also clamped to the engine's subject-size limit, so compare it with the requested value before accepting a contract for unusually large inputs.

A contract covers one search, not the total work of iterating over or replacing every match in a subject.

## Important limitations

- Revera operates on UTF-8 strings and does not accept arbitrary byte strings.
- Pattern backreferences and Perl-compatible extensions are not part of POSIX ERE and are rejected.
- The Rust API does not expose the `REG_NOTBOL` and `REG_NOTEOL` execution flags from `regexec()`.
- Match positions are byte offsets, not character counts.
- The embedded locale data makes locale-aware behavior self-contained, but it also contributes to the crate and binary size.

## Verification and license

The Rust library wraps the same generated engine used by the other Revera implementations.

The project cross-checks their matches, errors, replacements, iteration, and resource reports with a shared conformance suite.

The Lean development provides machine-checked semantics and proofs with a deliberately narrower scope, documented in the [Lean README](https://github.com/oneregex/revera/blob/main/lean/README.md).

Revera is licensed under the [MIT license](https://github.com/oneregex/revera/blob/main/LICENSE).
The embedded Unicode and CLDR data is covered by the [Unicode License v3](https://github.com/oneregex/revera/blob/main/LICENSES/Unicode-3.0.txt).
