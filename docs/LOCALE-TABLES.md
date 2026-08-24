# Generated locale tables

This directory supplies the `LC_CTYPE` and ERE-relevant `LC_COLLATE`
operations required by the
[POSIX ERE specification](POSIX-1-2024-ERE-SPECIFICATION.md) without using the
host's locale database. The committed data is generated from CLDR 48.2 and the
Unicode 17.0.0 data embedded in its `cldr-tools` jar.

## Coverage

The generated index contains all 1,122 locale identifiers represented by
`common/main/*.xml` in CLDR 48.2. Locale inheritance is resolved by ICU while
generating the tables. The index also exposes every collation type that CLDR
reports for each locale, including inherited types. Its 5,782 locale/type
selectors reduce to 138 source profiles and 117 distinct runtime data profiles.
The `C` and `POSIX` names are additional aliases for the POSIX locale and are
not part of that count.

Locale names are Unicode locale identifiers written with `-` or `_`
separators. Matching is ASCII case-insensitive, and a `.UTF-8` or `.UTF8`
suffix is accepted. The runtime is Unicode-scalar based; UTF-8 decoding belongs
at the regular-expression API boundary. No legacy character encoding is
provided. A `@collation=TYPE` modifier is accepted when the separate collation
argument is empty; other modifiers and duplicate type selections are rejected.

## `LC_CTYPE` projection

CLDR does not define POSIX character classes. The non-POSIX locale tables use
the Unicode 17 properties carried by CLDR 48.2 with these fixed rules:

- `alpha`, `upper`, `lower`, and `space` use the Unicode `Alphabetic`,
  `Uppercase`, `Lowercase`, and `White_Space` properties.
- `blank`, `graph`, and `print` use ICU's Unicode POSIX compatibility
  properties.
- `cntrl` contains general category `Cc`.
- `punct` is `graph` minus `alpha` and `digit`.
- As POSIX requires, `digit` is only `0` through `9`, `xdigit` adds only
  `A` through `F` and `a` through `f`, and `alnum` is `alpha` plus `digit`.

The class masks use a two-stage 256-scalar trie. CLDR locales share its 152
deduplicated data blocks. Simple one-scalar Unicode upper/lower mappings are
stored once. Turkish and Azerbaijani select a small override profile for
dotted and dotless I. Multi-scalar Unicode case mappings are intentionally not
POSIX case counterparts.

The POSIX locale has its required portable classes and ASCII one-character
case mappings only. Every Unicode scalar remains a one-character collating
element. Scalars after U+007F have unique primary weights after the portable
set, so scalar order implements the required POSIX-locale collation order.

## `LC_COLLATE` projection

The regular-expression specification needs three collation operations: validate
a collating element, find a longest matching multi-character element, and test
primary equivalence. It does not need general string sorting. Outside the POSIX
locale, range behavior is unspecified; this implementation exposes the
permitted reject policy through `rv_locale_supports_ranges()`.

Generation asks the CLDR 48.2 ICU implementation for each effective locale
collator's contractions and primary-strength sort keys. Context-before rules
are not contractions because their prefix is already-consumed context, not part
of one collating element. Canonical contraction closures produced by CLDR are
included.

The root contraction set and all non-singleton root primary equivalence classes
are stored once. A locale profile stores only added or removed contractions and
members of equivalence classes whose membership differs from root. Elements
absent from an equivalence table form singleton classes. Lookups use binary
search over sorted integer IDs and require no allocation or initialization.

## Reproduction

Download these CLDR 48.2 release artifacts from
`https://www.unicode.org/Public/cldr/48.2/`:

- `cldr-common-48.2.zip`
- `cldr-tools-48.2.jar`

Then run:

```sh
tools/generate-locale-data.sh \
  /path/to/cldr-common-48.2.zip \
  /path/to/cldr-tools-48.2.jar \
  /path/to/jdk
```

The script requires JDK 17 or later only at generation time and verifies both
artifacts by SHA-512 before running. The CLDR tools manifest identifies code
commit `11299982335beb974c1c63c45265184e759c0f41`, tagged as `release-48-2`
in the CLDR repository. The embedded library reports ICU 78.2 and Unicode
17.0.0.

The published CLDR 48.2 `SHASUM512.txt` assigns the jar hash to
`cldr-common-48.2.zip`. The downloaded common archive instead has the SHA-512
below, which the same manifest lists for `core.zip`:

```text
de8660f5371e0fcfd03a42e3b4fc4c686ec6cd602b402f1e3d227844005a54eb7
952873894443523837d5828c42874a1a267a19f91ded207a2d166144791fa62
```

The two displayed lines form one digest. The generator pins that observed
archive hash and separately checks the jar's published hash.

The generated data is covered by the Unicode License v3 in
[`LICENSES/Unicode-3.0.txt`](../LICENSES/Unicode-3.0.txt).
