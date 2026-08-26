# Vego: a mechanically translatable subset of Go

Vego is a strict subset of Go. Every Vego program is a valid Go
program with the same meaning. The subset is small enough to parse
with the standard `go/ast` package and a short rule checker, and it
avoids every Go feature that has no direct, zero-overhead counterpart
in C++, Rust, or Zig.

The name stands for "verifiable, exportable Go". The subset exists so
that one implementation can be written once, exported to a JSON form,
translated into other languages, and proven correct with a theorem
prover. Nothing in the subset depends on a garbage collector, on
goroutines, or on the Go runtime.

This document is the complete specification. A program that follows
every rule here, and that compiles with the standard Go toolchain, is
a valid Vego program. The Go compiler supplies the type checking; the
`vego2json` tool enforces the subset rules and emits the JSON form.

## 1. Program structure

A Vego program is one Go package in one directory.

- Every `.go` file in the directory belongs to the program, except
  host files.
- A host file is a file whose name ends in `_host.go` or `_test.go`.
  Host files are ordinary Go, outside the subset. They supply what the
  subset cannot express on purpose: embedded data, test harnesses,
  and convenience wrappers. A translation of the program to another
  language must replace the host files with equivalents; this is the
  "minimal runtime" of that language.
- Subset files must not contain any `import` declaration. The subset
  is hermetic: builtin operations only.
- The package must compile with `go build` and be clean under
  `go vet` and `gofmt`.

Top-level declarations allowed in subset files:

- `const` declarations of typed or untyped scalar constants.
- `type` declarations of struct types.
- `var` declarations of package-level immutable data.
- `func` declarations of plain functions.

Nothing else is allowed at the top level. In particular there are no
methods, no interfaces, no type aliases, no named scalar types, and no
generic declarations.

## 2. Types

### 2.1 Scalar types

| Type   | Meaning                                     |
|--------|---------------------------------------------|
| bool   | truth value                                 |
| uint8  | unsigned 8-bit integer (byte is not used)   |
| uint16 | unsigned 16-bit integer                     |
| uint32 | unsigned 32-bit integer                     |
| uint64 | unsigned 64-bit integer                     |
| int32  | signed 32-bit integer                       |
| int64  | signed 64-bit integer                       |
| int    | signed integer, exactly 64 bits wide        |

`int` is the index type: `len`, `cap`, and index expressions use it.
A Vego program must only run on targets where `int` is 64 bits, and
translators must map it to a 64-bit signed type. Rust translators cast
to `usize` at index sites.

The Go types `rune`, `byte`, `uintptr`, `float32`, `float64`,
`complex64`, `complex128`, and the platform-width `uint` are not in
the subset. Character values use `int32`. There is no floating point.

Unsigned arithmetic wraps, as in Go. Signed arithmetic also wraps,
two's complement, as in Go; translators to C++ must compute through
unsigned types and convert back. Division and remainder truncate
toward zero and must not divide by zero. Shift counts must be smaller
than the operand width.

### 2.2 Strings

`string` is an immutable byte sequence. A string value is a view: it
can be copied, sliced, indexed, and compared freely, and it never
changes. Because strings are immutable, sharing them is safe in every
target language; a translator maps them to a pointer-and-length pair
(`&[u8]`, `std::string_view`, `[]const u8`) or to an owned immutable
buffer, whichever fits its memory management.

`s[i]` has type `uint8`. `s[i:j]` is a substring view. `len(s)` is the
byte count. Strings compare with `==`, `!=`, `<`, `<=`, `>`, `>=` in
byte order. There is no range loop over a string (that would decode
UTF-8 implicitly); programs decode bytes explicitly.

### 2.3 Arrays and slices

`[N]T` is a fixed-size array with value semantics. `N` is a constant
expression. Arrays copy on assignment; large arrays should be passed
through a struct pointer parameter.

`[]T` is a growable buffer of `T`. The element type can be any subset
type, including another slice type or a struct. The zero value is the
empty slice. Slices follow the buffer model of section 6.

### 2.4 Structs

A struct type declares named fields. A field can have any subset type
except a pointer type or a function type. Struct values copy on
assignment; section 6 restricts the copying of structs that contain
slices.

### 2.5 Pointers

The only pointer type is `*S` where `S` is a named struct type, and it
can appear in exactly two places:

- as the type of a function parameter,
- as the operand of a field access or index inside that function.

A pointer parameter is a borrow: the function can read and write
through it during the call, and must not store it anywhere. There are
no pointers in struct fields, no pointers in slices, no pointer
returns, no pointer locals, no `&` on slice elements, and no `new`.
Callers pass `&v`, where `v` is an addressable struct variable or
field, directly as a call argument; `&` appears in no other
position, so a pointer can never outlive the call it serves.

## 3. Declarations

### 3.1 Constants

```go
const dupMax = 255
const lenInf int = 1 << 30
```

A constant is a scalar or string literal expression, possibly built
from other constants with the operators of section 5. `iota` is not in
the subset; every constant is written out. Grouped `const (...)`
blocks are allowed; each entry needs an explicit value.

### 3.2 Package-level variables

A package-level `var` declares immutable data: a scalar, a string, or
an array, with a constant initializer (a literal, possibly nested).
Slice-typed globals are not in the subset. No statement may assign to
a global, in whole or in part, and no expression may create a
writable view of one: a global is never the operand of `&`, never
sliced, and never passed as a slice. Translators emit it as static
constant data.

### 3.3 Functions

```go
func name(p1 T1, p2 *S, buf []int32) (int32, bool) { ... }
```

- Zero or more parameters. Parameter types: any subset type, plus
  `*S` borrows.
- Zero, one, or two results. No named results. A function that needs
  more than two results returns a struct.
- No variadic parameters, no methods, no nested function literals,
  no recursion limit (recursion is allowed and used).

## 4. Statements

Allowed statements, with their exact Go form:

- Declaration: `var x T`, `var x T = e`, `x := e`.
- Assignment: `lhs = e`, and compound `lhs += e`, `-=`, `*=`, `/=`,
  `%=`, `|=`, `&=`, `^=`, `<<=`, `>>=`, `&^=` on scalars.
- Two-value forms, only from a two-result call:
  `a, b := f(...)`, `a, b = f(...)`. The blank identifier `_` can
  discard either value. No other multi-assignment exists; there is no
  tuple swap.
- Increment and decrement: `i++`, `i--`.
- `if cond { } else if cond { } else { }`. No init statement in the
  condition (`if x := f(); ...` is not in the subset).
- `for` in three forms:
  - `for cond { }`
  - `for i := e; cond; post { }`
  - `for i := range e { }` and `for i, v := range e { }` where `e` is
    a slice, an array, or an `int` count. `v` is a copy of the
    element; if the element type contains a slice, only the `i` form
    is allowed. `for range e { }` with no variables is also allowed.
- `switch tag { case c1, c2: ... default: ... }` where `tag` is a
  scalar expression and every case value is a constant. No
  `fallthrough`, no empty-tag `switch {}`, no init statement, no type
  switch.
- `break` and `continue`, without labels. Both refer to the nearest
  enclosing loop. A `break` whose nearest breakable statement is a
  `switch` is not in the subset; Vego cases never fall through, so
  such a `break` has no use.
- `return`, `return e`, `return e1, e2`.
- Blocks `{ ... }` only as bodies of the statements above.

Not in the subset: `goto`, labels, `defer`, `go`, `select`, `range`
over strings, maps or channels, `fallthrough`.

## 5. Expressions

- Literals: decimal and hexadecimal integers, `'c'` character
  literals (type-checked as int32 constants), `true`, `false`,
  interpreted string literals (`"..."`), raw string literals.
- Operators on integers: `+ - * / % << >> & | ^ &^`, unary `-` and
  `^`. On booleans: `&& || !`. Comparisons: `== != < <= > >=`.
  `&&` and `||` short-circuit.
- Conversions: `T(x)` between scalar types (Go truncation and sign
  rules), `string(b)` from `[]uint8` (copies and freezes),
  `[]uint8(s)` from string (copies into a fresh buffer). No other
  conversions; in particular `string(i)` from an integer is not in
  the subset.
- Index `a[i]`, slice `a[i:j]` (`a[i:]`, `a[:j]` and `a[:]` are
  allowed as shorthand), field access `x.f`, call `f(args)`. No
  three-index slices. Slicing an addressable array yields a view of
  the array's memory; this is how a program builds a bounded scratch
  buffer without an allocation.
- Composite literals: `S{Field: v, ...}` with keys for structs,
  `[]T{v, ...}` and `[N]T{v, ...}` positional for slices and arrays.
  Nested composite literals follow the same rules.
- Address `&v`, only as a direct call argument, and only on struct
  variables or struct fields, never on globals or slice elements.

### 5.1 Builtin functions

| Builtin            | Rule                                          |
|--------------------|-----------------------------------------------|
| len(x)             | strings, slices, arrays                       |
| cap(x)             | slices                                        |
| append(s, e...)    | one or more element arguments                 |
| append(s, s2...)   | spread form: one slice argument, or a string  |
|                    | when the element type is uint8                |
| make([]T, n)       | zeroed buffer                                 |
| make([]T, n, c)    | zeroed buffer with capacity                   |
| copy(dst, src)     | overlap-safe (memmove); returns count or the  |
|                    | result is discarded                           |
| min(a, b)          | two arguments, same scalar type               |
| max(a, b)          | two arguments, same scalar type               |

`clear`, `new`, `panic`, `recover`, `print`, `println`, `delete`,
and `complex/real/imag` are not in the subset. Out-of-range indexing
and slicing abort the program in every target, as in Go; a correct
program never does it, and the planned LEAN4 proof shows exactly that.

## 6. The buffer model

The subset has no garbage collector to lean on, so it restricts how
slices flow. The model gives every buffer exactly one owner, which is
what lets translators free memory correctly, and it keeps borrows
transient, which is what lets them use plain pointers.

1. Every slice-typed variable and struct field is an owner. It owns
   the buffer produced by `make`, `append`, a composite literal, a
   conversion, or a function result.
2. Assignment to an owner is one of:
   - a fresh buffer: `make(...)`, `append(...)`, a composite
     literal, `[]uint8(s)`, or a call result;
   - a move: a local variable, or a field of a local, whose value is
     not used again;
   - a truncation of the same owner: `x = x[:k]` or `x = x[i:j]`
     where the right-hand side slices `x` itself, and equally
     `x.f = x.f[:k]`;
   - a self-append: `x = append(x, ...)`, including the reset form
     `x = append(x[:0], ...)`.
3. Any other slice expression (`a[i:j]` of a different owner, a slice
   read from a field) is a view. A view can be indexed, sliced
   further, passed as an argument, and bound to a local, but it must
   not be assigned to a struct field, stored into a slice element, or
   returned, and it must not be used after the owner it was taken
   from has been assigned to or appended to.
4. Function parameters of slice type are views. A function may read
   and write elements of a slice parameter, and may not store the
   parameter itself anywhere that outlives the call.
5. A function result of slice type transfers ownership to the caller.
6. Two slice arguments to the same call may overlap; `copy` behaves
   like memmove. Element writes through one view are visible through
   another, exactly as in Go.
7. Strings are exempt: they are immutable, so any string view can be
   stored anywhere. A translator that frees string buffers gives the
   stored copy shared or duplicated storage; both are correct because
   the bytes never change.
8. Move-through parameters: a function may take a buffer, grow it
   with `append`, shrink it, and return it, in the style of Go's
   `append` itself. The caller must pass an owner (or its `[:0]`
   reset) in that argument position and must reassign the same owner
   from the result in the same statement: `x = f(x, ...)`. During the
   call the parameter behaves as a view; the return moves ownership
   back.
9. A struct that contains slice fields moves like a slice: it can be
   assigned only from a fresh composite literal, a call result, or a
   local that is not used again (for example, appending a
   just-initialized struct into an arena slice). It must not be
   copied while both copies stay live. Structs without slice fields
   copy freely.

Capacity after growth is target defined. A growing `append` gives
the buffer at least the needed capacity and a zeroed spare region.
The translated runtimes and the LEAN4 model allocate exactly
`max(2*cap, 8, need)` elements. The Go original runs on Go's own
`append`, whose capacities differ. A conforming program may read
`cap` after a growing append, but must not let the value reach
observable output. Capacities from `make` and from slicing are
exact and identical in every target.

The `vego2json` tool checks what is decidable locally: writes into
slice-typed fields and elements must be fresh buffers, moves, or
self-truncations; package-level data is never written; `&` stays on
struct variables and fields. The "not used again" move clauses and
rule 9 stay with the program author, and the LEAN4 model gives the
final word.

## 7. Semantics

Vego semantics are Go semantics, restricted:

- Evaluation order is left to right; function arguments evaluate in
  order.
- All values are zero-initialized: integers to 0, bools to false,
  strings to "", slices to empty, structs field by field.
- There is one thread. Nothing in the subset synchronizes.
- Abnormal termination (index out of range, division by zero,
  impossible shift) aborts the program. It is not recoverable and
  carries no defined value.

## 8. The JSON representation

`vego2json` turns one Vego package into one JSON object. The JSON is
a faithful syntax tree: translating it back to Go text reproduces the
program up to formatting and comments. Every node is an object with a
`"k"` field naming its kind. Positions are omitted; the JSON is the
portable artifact, not a diagnostic format.

Top level:

```json
{
  "vego": 1,
  "package": "revera",
  "consts": [ ... ],
  "vars": [ ... ],
  "types": [ ... ],
  "funcs": [ ... ]
}
```

Types appear in declaration order; the checker guarantees that order
compiles. Every list below uses the shapes that follow.

Type references:

```json
{"k": "named",  "name": "bool" | "uint8" | ... | "int" | "string"}
{"k": "slice",  "elem": TYPE}
{"k": "array",  "len": EXPR, "elem": TYPE}
{"k": "struct_ref", "name": "Regexp"}
{"k": "ptr",    "name": "Regexp"}
```

Declarations:

```json
{"k": "const", "name": "dupMax", "type": TYPE | null, "value": EXPR}
{"k": "var",   "name": "classNames", "type": TYPE | null, "value": EXPR}
{"k": "type",  "name": "Match",
 "fields": [{"name": "So", "type": TYPE}, ...]}
{"k": "func",  "name": "compile",
 "params":  [{"name": "pattern", "type": TYPE}, ...],
 "results": [TYPE, ...],
 "body": [STMT, ...]}
```

Statements:

```json
{"k": "var_decl", "name": "x", "type": TYPE | null, "value": EXPR | null}
{"k": "define",   "names": ["a"] | ["a","b"], "value": EXPR}
{"k": "assign",   "lhs": [EXPR] | [EXPR,EXPR], "value": EXPR}
{"k": "op_assign","op": "+=", "lhs": EXPR, "value": EXPR}
{"k": "incdec",   "op": "++" | "--", "lhs": EXPR}
{"k": "if",       "cond": EXPR, "then": [STMT,...],
                  "else": [STMT,...] | null}
{"k": "for",      "init": STMT | null, "cond": EXPR | null,
                  "post": STMT | null, "body": [STMT,...]}
{"k": "range",    "idx": "i" | "_" | null, "val": "v" | null,
                  "over": EXPR, "body": [STMT,...]}
{"k": "switch",   "tag": EXPR,
                  "cases": [{"values": [EXPR,...], "body": [STMT,...]}],
                  "default": [STMT,...] | null}
{"k": "break"} {"k": "continue"}
{"k": "return",   "values": [EXPR,...]}
{"k": "expr_stmt","value": EXPR}
{"k": "block",    "body": [STMT,...]}
```

In `define` and `assign`, a two-name form always has a call on the
right. `_` appears literally where the program discards a value.

Expressions:

```json
{"k": "int",    "value": "255"}
{"k": "char",   "value": "97"}
{"k": "str",    "value": "..."}
{"k": "bool",   "value": true}
{"k": "ident",  "name": "x"}
{"k": "field",  "x": EXPR, "name": "f"}
{"k": "index",  "x": EXPR, "index": EXPR}
{"k": "slice_expr", "x": EXPR, "lo": EXPR | null, "hi": EXPR | null}
{"k": "call",   "fn": "name", "args": [EXPR,...]}
{"k": "builtin","fn": "len" | "cap" | "append" | "make" | "copy"
                    | "min" | "max",
                "args": [EXPR,...], "spread": bool,
                "type": TYPE | null}
{"k": "conv",   "type": TYPE, "x": EXPR}
{"k": "unary",  "op": "-" | "^" | "!" | "&", "x": EXPR}
{"k": "binary", "op": "+", "x": EXPR, "y": EXPR}
{"k": "composite", "type": TYPE,
   "fields": [{"name": "f", "value": EXPR}, ...] |
   "elems":  [EXPR, ...]}
```

Integer and character literal values are decimal strings, so 64-bit
values survive JSON readers that parse numbers as doubles. `builtin`
carries the element or slice type in `"type"` when the builtin is
`make` (the made type) so translators do not need inference for
allocation. `conv` covers scalar conversions and the two
string/buffer conversions (`"type"` is then the target).

The tool refuses any construct outside this grammar, with a file and
line diagnostic. The JSON therefore doubles as the subset's machine
definition: what the tool emits is exactly what a translator and the
LEAN4 model must handle.

## 9. Translation notes

The subset was chosen so each construct has a direct target form:

| Vego                 | C++                | Rust                | Zig                  |
|----------------------|--------------------|---------------------|----------------------|
| int / int64          | int64_t            | i64                 | i64                  |
| int32 / uint8 ...    | int32_t, uint8_t   | i32, u8             | i32, u8              |
| string               | std::string_view*  | &[u8]*              | []const u8*          |
| []T                  | std::vector<T>     | Vec<T>              | std.ArrayList(T)     |
| [N]T                 | std::array<T,N>    | [T; N]              | [N]T                 |
| struct               | struct             | struct              | struct               |
| *S parameter         | S&                 | &mut S              | *S                   |
| slice parameter      | std::span<T>       | &mut [T] or raw     | []T                  |
| multiple results     | struct / pair      | tuple               | struct               |
| append               | push_back / insert | extend / push       | appendSlice / append |
| copy                 | memmove            | copy_within / copy  | @memmove             |
| abort on bad index   | assert / at()      | panic (checked)     | safety check         |

(*) An owned result string (rule 6.5) becomes std::string, String, or
an allocated []u8 in the target; the exemption of rule 6.7 makes both
representations valid for stored strings.

Views can alias, so a borrow-checked target either copies at the few
aliasing call sites or lowers slices to pointer-and-length pairs in
generated (unsafe) code. The correctness argument for generated code
is the LEAN4 proof over the JSON, not the target language's checker.

## 10. Conformance

A conforming Vego program:

1. contains only the constructs of sections 1 through 5,
2. respects the buffer model of section 6,
3. compiles with the standard Go toolchain, and
4. passes `vego2json` with no diagnostics.

The reference implementation of the checker and exporter lives in
`cmd/vego2json` next to this file. The `revera` package in this
directory is the first conforming program: a complete POSIX.1-2024
ERE engine.
