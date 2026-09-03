# Revera for Zig

Revera is a clean-room implementation of POSIX.1-2024 extended regular expressions for Zig.
It uses POSIX leftmost-longest matching, so it is useful when a pattern must behave like `regcomp()` and `regexec()` instead of a Perl-style regex engine.
Patterns and subjects are UTF-8 byte slices.
Match positions and compile error positions are byte offsets.

[Source repository](https://github.com/oneregex/revera) | [Changelog](https://github.com/oneregex/revera/blob/main/CHANGELOG.md)

## Requirements

The package requires Zig `0.17.0` or newer and has no external dependencies.
Its CLDR locale data is embedded in the module.

## Add the dependency

To use a source checkout today, point a path dependency at its `zig` directory:

```zig
.dependencies = .{
    .revera = .{
        .path = "revera/zig",
    },
},
```

The path is relative to your project's `build.zig.zon`.

Pass the standard target option to Revera, then import its public module:

```zig
const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const revera_dep = b.dependency("revera", .{
        .target = target,
    });

    const exe = b.addExecutable(.{
        .name = "example",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/main.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    exe.root_module.addImport("revera", revera_dep.module("revera"));
    b.installArtifact(exe);
}
```

With this setup, Revera builds in Debug mode by default.

A standard `zig build --release=safe` request selects Revera's preferred `ReleaseSafe` mode.

## Complete example

```zig
const std = @import("std");
const revera = @import("revera");

pub fn main() !void {
    const allocator = std.heap.page_allocator;

    var re = try revera.Regex.compile(allocator, "([a-z]+)([0-9]*)", .{});
    defer re.deinit();

    const subject = "__abc12__";
    var captures = (try re.captures(subject)) orelse return error.NoMatch;
    defer captures.deinit();

    std.debug.print("whole: {s}\nword: {s}\ndigits: {s}\n", .{
        captures.get(0).?.text(),
        captures.get(1).?.text(),
        captures.get(2).?.text(),
    });
}
```

This prints:

```text
whole: abc12
word: abc
digits: 12
```

Group 0 is always the whole match.
A group that did not participate is `null`.

## Public API

`@import("revera")` exposes the following main types and values:

| API                                                | Purpose                                                   |
| -------------------------------------------------- | --------------------------------------------------------- |
| `Regex.compile(allocator, pattern, options)`       | Compiles a pattern and copies it into owned storage.      |
| `Regex.deinit()`                                   | Releases the compiled expression.                         |
| `Regex.groupCount()`                               | Returns the number of reported groups, including group 0. |
| `Regex.isMatch(subject)`                           | Reports whether the pattern matches anywhere.             |
| `Regex.find(subject)`                              | Returns the leftmost-longest `Match`, or `null`.          |
| `Regex.captures(subject)`                          | Returns owned `Captures`, or `null`.                      |
| `Regex.matches(subject)`                           | Creates an iterator over non-overlapping `Match` values.  |
| `Regex.captureMatches(subject)`                    | Creates an iterator that returns owned `Captures` values. |
| `Regex.replaceAll(subject, replacement)`           | Replaces every non-overlapping match.                     |
| `Regex.replaceFirstN(subject, replacement, limit)` | Replaces at most `limit` matches.                         |
| `Regex.contract(max_input)`                        | Returns resource bounds for one search.                   |
| `Locale.posix()`                                   | Returns the default POSIX locale.                         |
| `Locale.open(allocator, name, collation_type)`     | Looks up a locale in the embedded CLDR data.              |
| `Locale.names(allocator)`                          | Allocates the outer slice of available locale names.      |
| `embedded_locale_data`                             | Contains the embedded CLDR locale blob.                   |
| `dup_max`                                          | Gives the largest accepted interval count.                |

`Match` contains `start` and `end` byte offsets.
Its `text()`, `len()`, and `isEmpty()` methods provide the matched bytes, byte length, and empty-match status.

`Captures.get(index)` returns a `Match` or `null` when the group did not participate or the index does not exist.

`Captures.len()` includes group 0.

Both iterators use `next()` and return `null` when finished.

They report non-overlapping matches from left to right and make progress after an empty match.

## Compile options

`revera.Options` has these fields:

| Field               | Default | Effect                                                                                              |
| ------------------- | ------- | --------------------------------------------------------------------------------------------------- |
| `case_insensitive`  | `false` | Matches upper and lower case alike using the selected locale.                                       |
| `newline_sensitive` | `false` | Gives `^` and `$` their line meaning and prevents dot or a negated bracket from matching a newline. |
| `no_captures`       | `false` | Compiles for `isMatch()` only. APIs that need match offsets return `error.NoCaptures`.              |
| `shortest_match`    | `false` | Makes repetitions prefer their shortest match, like POSIX `REG_MINIMAL`.                            |
| `locale`            | `null`  | Selects bracket, collation, and case behavior. `null` means POSIX.                                  |
| `error_position`    | `null`  | Receives the byte offset where pattern compilation stopped when that position is available.         |

For example:

```zig
var at: usize = 0;
var re = try revera.Regex.compile(allocator, "^hello$", .{
    .case_insensitive = true,
    .newline_sensitive = true,
    .error_position = &at,
});
defer re.deinit();
```

The value behind `error_position` is left unchanged when an error has no position.

## Replacement syntax

Replacement text follows `sed`-style rules:

- `&` inserts the whole match.
- `\1` through `\9` insert a capturing group.
- A backslash escapes the next character, so `\&` inserts a literal ampersand and `\\` inserts a literal backslash.

The returned replacement buffer belongs to the allocator passed to `Regex.compile()`.
Free it with that allocator.

```zig
var pair = try revera.Regex.compile(allocator, "([a-z]+)([0-9]+)", .{});
defer pair.deinit();

const out = try pair.replaceAll("abc12", "[&:\\1]");
defer allocator.free(out);
```

## Locales

The default locale is POSIX, also known as the C locale.
Use `Locale.open()` when bracket expressions, collating elements, equivalence classes, or case folding need CLDR locale behavior:

```zig
const cs = (try revera.Locale.open(allocator, "cs", "")) orelse
    return error.UnknownLocale;

var re = try revera.Regex.compile(allocator, "[[.ch.]]", .{ .locale = cs });
defer re.deinit();
```

An empty collation type selects the locale's standard collation.

`Locale.open()` returns `null` for an unknown locale or collation type.
The returned `Locale` points into embedded data, owns no memory, and needs no cleanup.

`Locale.names()` allocates only the outer slice.
Free that slice with the same allocator, but do not free the names inside it because they point into embedded data.

## Ownership and threads

The allocator passed to `Regex.compile()` backs the compiled expression, scratch allocations, captures, and replacement results.
It must remain valid until those values have been released.

- Call `Regex.deinit()` once for every successfully compiled expression.
- `Match` does not own memory and borrows its subject.
- `Captures` owns its group list and must be deinitialized, while its matches still borrow the subject.
- A match iterator borrows both the `Regex` and its subject for the iterator's lifetime.
- Every item from `captureMatches()` must be deinitialized separately.
- Replacement results must be freed directly with the allocator.

A compiled `Regex` keeps no mutable search state and may be searched concurrently.
Its allocator must be thread safe, and `deinit()` must not run while a search or iterator is active.
An iterator is mutable and should not itself be shared without synchronization.

## Errors and resource contracts

No match is not an error.
`find()` and `captures()` return `null`, iterators finish with `null`, and `isMatch()` returns `false`.

Allocation failures return `error.OutOfMemory`.
Engine failures use `revera.Error`, whose members are:

```text
InvalidPattern
InvalidCollatingElement
InvalidCharacterClass
InvalidEscape
InvalidBackReference
UnbalancedBracket
UnbalancedParenthesis
UnbalancedBrace
InvalidInterval
InvalidRange
OutOfCapacity
MissingRepeatOperand
NoCaptures
UnknownFailure
```

`OutOfCapacity` means an engine capacity limit was exceeded.
It is distinct from allocator exhaustion.

`Regex.contract(max_input)` reports bounds for a search over a subject of at most `max_input` bytes.
The result includes total `heap_bytes`, `stack_bytes`, and `steps`, plus separate matcher, one-pass capture, and general capture-solver figures.
Steps are abstract operations, not a time estimate.
Stack bytes are an estimate.
Figures saturate at `1 << 62`, which means the bound is too large to be useful.
The engine clamps `max_input` to `(1 << 31) - 1`.

## Regex behavior and limits

Revera implements POSIX ERE syntax and leftmost-longest matching.

It does not support backreferences or Perl escapes such as `\d` and `\w`.

`shortest_match` exposes the POSIX `REG_MINIMAL` compilation mode.

The package does not expose the `REG_NOTBOL` and `REG_NOTEOL` execution flags from `regexec()`.

Use `newline_sensitive` only when `^`, `$`, dot, and negated brackets should have POSIX `REG_NEWLINE` behavior.

Interval counts cannot exceed `revera.dup_max`.
Patterns and subjects should be valid UTF-8, and offsets always count bytes rather than Unicode scalar values.

## License

Revera is available under the MIT License.
The embedded Unicode and CLDR-derived data is covered by the Unicode License included in `LICENSES/Unicode-3.0.txt`.
