# Revera for TypeScript

Revera is a POSIX.1-2024 extended regular expression engine for JavaScript.

It uses leftmost-longest matching and accepts the same regular expression language as POSIX `regcomp()` and `regexec()`.

Use Revera when a pattern needs POSIX ERE behavior instead of JavaScript `RegExp` behavior.

Patterns do not support backreferences, lookaround, or Perl-style escapes such as `\d` and `\w`.

[Source repository](https://github.com/oneregex/revera)

## Requirements

Revera is published as an ES module.

It is intended for Node.js applications, not browsers.

Importing the package uses `node:fs` to load its bundled locale data.

Node.js 22.18 or later is required.

## Installation

```sh
npm install @oneregex/revera
```

## Quick start

```ts
import { Regex } from "@oneregex/revera";

const regex = new Regex("([a-z]+)([0-9]*)");
const captures = regex.captures("__abc12__");

if (captures === null) {
    throw new Error("expected a match");
}

console.log(captures.get(0)?.text); // abc12
console.log(captures.get(1)?.text); // abc
console.log(captures.get(2)?.text); // 12
```

A `Regex` is compiled once and can be reused for any number of subjects.
Searches do not keep state between calls.

## Matching

`test` reports whether the pattern matches anywhere in the subject.

`find` returns the first leftmost-longest match, while `captures` also returns its capturing groups.

Both result methods return `null` when there is no match.

```ts
import { Regex } from "@oneregex/revera";

const regex = new Regex("(a+)(b*)");

console.log(regex.test("xxaabyy")); // true
console.log(regex.find("xxaabyy")?.text); // aab

const captures = regex.captures("xxaabyy");
console.log(captures?.get(1)?.text); // aa
console.log(captures?.get(2)?.text); // b
```

Group 0 is the whole match.
`capturesLength` includes group 0, and `Captures.length` reports the same count for a result.
`Captures.get(index)` returns `null` if the group did not participate, the index is invalid, or the group does not exist.
A `Captures` value is iterable in group order.

Revera follows POSIX leftmost-longest rules.

For example, the expression below chooses `aa`, even though the first alternative could match `a`:

```ts
import { Regex } from "@oneregex/revera";

console.log(new Regex("a|aa").find("aa")?.text); // aa
```

Use `matches` or `captureMatches` to iterate over non-overlapping matches from left to right:

```ts
import { Regex } from "@oneregex/revera";

const regex = new Regex("(a+)(b*)");

for (const match of regex.matches("aab a aabbb")) {
    console.log(match.text);
}

for (const captures of regex.captureMatches("aab a")) {
    console.log(captures.get(1)?.text, captures.get(2)?.text);
}
```

The iterators perform their searches as they are consumed.

## Replacing matches

`replaceAll` replaces non-overlapping matches and returns a string.

In replacement text, `&` means the whole match, `\1` through `\9` refer to capturing groups, and a backslash escapes the next character.

Remember that a backslash must also be escaped inside a normal JavaScript string.

```ts
import { Regex } from "@oneregex/revera";

const regex = new Regex("(a+)(b*)");

console.log(regex.replaceAll("aab a aabbb", "<\\2\\1>"));
// <baa> <a> <bbbaa>

console.log(regex.replaceAll("aab a aabbb", "x", 2));
// x x aabbb

const upper = regex.replaceAllWith("aab a", (captures) =>
    captures.get(0)!.text.toUpperCase(),
);
console.log(upper); // AAB A
```

The optional `limit` passed to `replaceAll` and `replaceAllBytes` must be an integer.

A negative value, which is the default, replaces every match.
A nonnegative value replaces at most that many matches.

`replaceAllBytes` accepts the same arguments as `replaceAll` but returns a `Uint8Array`.

Use it when the subject may not be valid UTF-8 or when the result must remain byte-oriented.

`replaceAllWith` always returns a string and does not take a limit.

## Options

Pass an options object as the second argument to the constructor:

```ts
import { Regex } from "@oneregex/revera";

const line = new Regex("^world$", {
    caseInsensitive: true,
    newlineSensitive: true,
});

console.log(line.test("Hello\nWORLD")); // true
```

| Option             | Effect                                                                                                                                |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------- |
| `caseInsensitive`  | Matches upper and lower case alike, like `REG_ICASE`.                                                                                 |
| `newlineSensitive` | Gives `^` and `$` line-oriented behavior. A dot and a negated bracket expression do not match a newline, like `REG_NEWLINE`.          |
| `noCaptures`       | Compiles for `test` only, like `REG_NOSUB`. Methods that need match offsets or groups throw a `RegexError` with kind `"no-captures"`. |
| `shortestMatch`    | Makes every repetition prefer the shortest match instead of the usual longest match.                                                  |
| `locale`           | Selects the locale used for bracket expressions and case folding. The default is the POSIX locale.                                    |

The execution flags `REG_NOTBOL` and `REG_NOTEOL` are not exposed.

## Locales

The package includes CLDR locale data for character classes, case folding, collating elements, and equivalence classes.
`Locale.open` returns `null` when the locale name or collation type is unknown.

```ts
import { Locale, Regex } from "@oneregex/revera";

const czech = Locale.open("cs");
if (czech === null) {
    throw new Error("Czech locale data is unavailable");
}

const regex = new Regex("[[.ch.]]", { locale: czech });
console.log(regex.find("xchx")?.text); // ch
```

`Locale.open(name, collationType)` selects a named collation type.

Omitting the second argument selects the locale's standard collation.

`Locale.posix()` returns the POSIX locale, and `Locale.names()` lists the locale names in the bundled data.

`embeddedLocaleData()` returns a copy of the bundled locale blob for applications that need to inspect or store it.

Normal matching does not require calling this function.

## Strings, bytes, and offsets

Patterns and subjects accept either a `string` or a `Uint8Array`.

Strings are encoded as UTF-8 before the engine sees them.

A `Uint8Array` is used directly, which allows matching data that is not valid UTF-8.

Every offset is a byte offset, not a JavaScript UTF-16 string index:

```ts
import { Regex } from "@oneregex/revera";

const match = new Regex("b+").find("éb");
console.log(match?.start); // 2
console.log(match?.end); // 3
```

A `Match` exposes these properties:

| Property  | Meaning                                   |
| --------- | ----------------------------------------- |
| `start`   | Byte offset where the match starts.       |
| `end`     | Byte offset immediately after the match.  |
| `length`  | Length of the match in bytes.             |
| `isEmpty` | Whether the match is empty.               |
| `bytes`   | A `Uint8Array` view of the matched bytes. |
| `text`    | The matched bytes decoded as UTF-8.       |
| `subject` | The complete byte-oriented subject.       |

`Match.toString()` returns the same value as `Match.text`.

`Match.bytes` is a view, not a copy.
Call `match.bytes.slice()` if the bytes need independent storage.

When the input is a `Uint8Array`, matches retain that array as their subject.

Do not modify the array while a search or match result is in use unless that behavior is intentional.

## Errors

Invalid patterns and engine failures throw `RegexError`.

The error message contains a human-readable description and includes a byte offset when one is available.

```ts
import { Regex, RegexError } from "@oneregex/revera";

try {
    new Regex("[z-a]");
} catch (error) {
    if (error instanceof RegexError) {
        console.error(error.kind); // range
        console.error(error.offset);
    } else {
        throw error;
    }
}
```

`RegexError` provides:

| Property | Meaning                                                            |
| -------- | ------------------------------------------------------------------ |
| `kind`   | A kebab-case error category based on the POSIX `<regex.h>` errors. |
| `code`   | The engine's numeric error code.                                   |
| `offset` | The relevant UTF-8 byte offset, or `null` when no offset applies.  |

The possible error kinds are `"pattern"`, `"collating-element"`, `"character-class"`, `"escape"`, `"back-reference"`, `"bracket"`, `"paren"`, `"brace"`, `"interval"`, `"range"`, `"capacity"`, `"repeat"`, `"no-captures"`, and `"unknown"`.

Compilation offsets refer to the pattern.

Escape and backreference errors produced while parsing replacement text refer to the replacement.

A search usually fails with a `"capacity"` error when its subject exceeds the engine's capacity for that pattern.
The same error is used to fail closed if a compile-time-selected one-pass capture walk detects an internal inconsistency.

Numeric arguments such as a replacement limit or contract size throw `RangeError` when they are not integers.

## Resource contracts

`contract(maxInput)` reports conservative costs for one search on a subject no longer than `maxInput` bytes.

This lets an application check a pattern before accepting it for use with bounded input.

```ts
import { Regex } from "@oneregex/revera";

const regex = new Regex("(a|b)*c");
const contract = regex.contract(64 * 1024);

console.log(contract.hasOnePass);
console.log(contract.hasSolver);
console.log(contract.heapBytes);
console.log(contract.stackBytes);
console.log(contract.steps);
```

`heapBytes`, `stackBytes`, and `steps` are `bigint` values so their bounds remain exact.

`heapBytes` bounds explicit heap allocation, `stackBytes` estimates the deepest call stack, and `steps` bounds abstract unit-cost operations rather than elapsed time.
The heap figure covers fixed-width allocation requests, not total process memory.
Capture figures include conservative allocator-rounding allowances.
Runtime object headers, general allocator metadata, map buckets, and JavaScript garbage-collector bookkeeping are outside the model.

`maxInput` must be an integer.
Values below zero are clamped to zero, and values above `(1 << 31) - 1` are clamped to that engine limit.
The returned object does not expose the effective clamped value, so callers that require an exact input ceiling should validate the requested range before calling `contract`.

`hasOnePass` reports that captures use the compile-time selected one-pass walk.
`hasSolver` reports that they require the general solver.
The flags are mutually exclusive, and both are false when Phase B cannot run, such as for an expression without parenthesized subexpressions or one compiled with `noCaptures`.

## API summary

The package exports the following public values and types:

| Export               | Purpose                                                                                            |
| -------------------- | -------------------------------------------------------------------------------------------------- |
| `Regex`              | Compiles a POSIX ERE and provides matching, iteration, replacement, and resource-contract methods. |
| `RegexOptions`       | Constructor options for matching behavior and locale selection.                                    |
| `RegexError`         | Structured compilation, replacement, and search error.                                             |
| `ErrorKind`          | Union of the string values used by `RegexError.kind`.                                              |
| `Match`              | One matched byte span and its decoded text.                                                        |
| `Captures`           | The whole match and its capturing groups.                                                          |
| `Contract`           | Resource bounds returned by `Regex.contract`.                                                      |
| `Locale`             | POSIX and bundled CLDR locale selection.                                                           |
| `Text`               | Alias for string or `Uint8Array` input.                                                            |
| `DUP_MAX`            | Largest supported interval bound. It is currently `255`.                                           |
| `embeddedLocaleData` | Returns a copy of the package's locale data.                                                       |

All matching and replacement methods are synchronous.

## License

Revera is distributed under the MIT license.

The bundled Unicode and CLDR-derived data has its applicable notice in `LICENSES/Unicode-3.0.txt`.
