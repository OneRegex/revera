# Generated locale tables

These tables supply the `LC_CTYPE` and ERE-relevant `LC_COLLATE` operations that the [POSIX ERE specification](POSIX-1-2024-ERE-SPECIFICATION.md) requires, without the host locale database.

The committed data is generated from CLDR 48.2 and from the Unicode 17.0.0 data embedded in its `cldr-tools` jar.

## Coverage

The generated index holds all 1,122 locale identifiers that `common/main/*.xml` represents in CLDR 48.2.
ICU resolves locale inheritance while the tables are generated.
The index also exposes every collation type that CLDR reports for each locale, inherited types included.
Its 5,782 locale and type selectors reduce to 138 source profiles and 117 distinct runtime data profiles.
The `C` and `POSIX` names are extra aliases for the POSIX locale, and they are outside that count.

Locale names are Unicode locale identifiers, written with `-` or `_` separators.
Matching is ASCII case-insensitive, and a `.UTF-8` or `.UTF8` suffix is accepted.
The runtime works on Unicode scalars, so UTF-8 decoding belongs at the API boundary of the regular-expression engine.
There is no legacy character encoding.
A `@collation=TYPE` modifier is accepted when the separate collation argument is empty.
Other modifiers, and a duplicate type selection, are rejected.

## `LC_CTYPE` projection

CLDR does not define the POSIX character classes.
The non-POSIX locale tables therefore use the Unicode 17 properties that CLDR 48.2 carries, under these fixed rules:

- `alpha`, `upper`, `lower` and `space` use the Unicode `Alphabetic`, `Uppercase`, `Lowercase` and `White_Space` properties.
- `blank`, `graph` and `print` use the Unicode POSIX compatibility properties of ICU.
- `cntrl` holds general category `Cc`.
- `punct` is `graph` without `alpha` and `digit`.
- POSIX fixes the last three: `digit` is `0` through `9`, `xdigit` adds only `A` through `F` and `a` through `f`, and `alnum` is `alpha` plus `digit`.

The class masks use a two-stage 256-scalar trie, and the CLDR locales share its 152 deduplicated data blocks.
The simple one-scalar Unicode case mappings are stored once.
Turkish and Azerbaijani select a small override profile for dotted and dotless I.
Multi-scalar Unicode case mappings are not POSIX case counterparts, and the tables leave them out on purpose.

The POSIX locale holds only its required portable classes and the ASCII one-character case mappings.
Every Unicode scalar stays a one-character collating element.
Scalars after U+007F have unique primary weights after the portable set.
Scalar order therefore gives the POSIX-locale collation order that POSIX requires.

## `LC_COLLATE` projection

The regular-expression specification needs three collation operations.
It must validate a collating element, find the longest matching multi-character element, and test primary equivalence.
It does not need general string sorting.
Range behavior outside the POSIX locale is unspecified, and this implementation exposes the permitted reject policy through `rv_locale_supports_ranges()`.

Generation asks the CLDR 48.2 ICU implementation for the contractions and primary-strength sort keys of each effective locale collator.
A context-before rule is not a contraction, because its prefix is already-consumed context and not part of one collating element.
The canonical contraction closures that CLDR produces are included.

The root contraction set and every non-singleton root primary equivalence class are stored once.
A locale profile stores only what differs from root: the added or removed contractions, and the members of the equivalence classes whose membership changed.
An element absent from an equivalence table forms a singleton class.
Lookups are binary searches over sorted integer IDs, and they need no allocation and no initialization.

## Reproduction

Download these CLDR 48.2 release artifacts from `https://www.unicode.org/Public/cldr/48.2/`:

- `cldr-common-48.2.zip`
- `cldr-tools-48.2.jar`

Then run:

```sh
cd locale && tools/generate-locale-data.sh \
  /path/to/cldr-common-48.2.zip \
  /path/to/cldr-tools-48.2.jar \
  /path/to/jdk
```

The script runs from `locale/` and writes `rv_locale_data.inc` there, unless a fourth argument names another output.
The script needs JDK 17 or later at generation time only.
It verifies both artifacts by SHA-512 before it runs.
The CLDR tools manifest identifies code commit `11299982335beb974c1c63c45265184e759c0f41`, tagged `release-48-2` in the CLDR repository.
The embedded library reports ICU 78.2 and Unicode 17.0.0.

The published CLDR 48.2 `SHASUM512.txt` assigns the jar hash to `cldr-common-48.2.zip`.
The downloaded common archive has the SHA-512 below instead, which the same manifest lists for `core.zip`:

```text
de8660f5371e0fcfd03a42e3b4fc4c686ec6cd602b402f1e3d227844005a54eb7
952873894443523837d5828c42874a1a267a19f91ded207a2d166144791fa62
```

The two displayed lines form one digest.
The generator pins that observed archive hash, and it checks the published hash of the jar separately.

The Unicode License v3 covers the generated data.
[`LICENSES/Unicode-3.0.txt`](../LICENSES/Unicode-3.0.txt) holds its text.
