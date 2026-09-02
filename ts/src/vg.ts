// Minimal runtime for the generated Vego engine.
// It supplies what the Go runtime supplied implicitly: growable buffers, immutable string views, and integer arithmetic with Go semantics.
//
// There is no memory context.
// The garbage collector owns every buffer, so the generated functions take no allocator parameter.
// The runtime holds no state apart from the allocation counters that the bench host reads.
//
// Integers are the one place where JavaScript cannot follow Go exactly.
// A Vego `int` is a 64-bit signed integer, and a JavaScript number is exact only up to 2^53.
// The engine keeps every `int` well below that, because they are lengths, offsets and counts.
// The printer therefore maps `int` and the 32-bit types to `number`, and checks every 64-bit result with `chk`.
// A result outside the exact range throws a RangeError instead of silently losing precision.
// `int64` and `uint64` map to `bigint`, so the resource contracts and the hash arithmetic stay exact.

export type Str = Uint8Array;

export const EMPTY: Str = new Uint8Array(0);

export class VegoError extends RangeError {}

function fail(message: string): never {
    throw new VegoError(message);
}

// lit turns a string literal of the generated code into bytes.
// The printer writes every byte as one character below 256, so the char codes are the bytes.
export function lit(s: string): Str {
    const out = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) {
        out[i] = s.charCodeAt(i);
    }
    return out;
}

// ix checks an index against a length and returns it, so a bad index aborts like it does in Go.
export function ix(i: number, len: number): number {
    if (i < 0 || i >= len) {
        fail("index out of range: " + i + " with length " + len);
    }
    return i;
}

export function strSub(s: Str, lo: number, hi: number): Str {
    if (lo < 0 || lo > hi || hi > s.length) {
        fail("string slice bounds out of range");
    }
    return s.subarray(lo, hi);
}

export function strHead(s: Str, hi: number): Str {
    return strSub(s, 0, hi);
}

export function strTail(s: Str, lo: number): Str {
    return strSub(s, lo, s.length);
}

export function streq(a: Str, b: Str): boolean {
    if (a.length !== b.length) {
        return false;
    }
    for (let i = 0; i < a.length; i++) {
        if (a[i] !== b[i]) {
            return false;
        }
    }
    return true;
}

export function strcmp3(a: Str, b: Str): number {
    const n = a.length < b.length ? a.length : b.length;
    for (let i = 0; i < n; i++) {
        if (a[i] !== b[i]) {
            return a[i] < b[i] ? -1 : 1;
        }
    }
    if (a.length !== b.length) {
        return a.length < b.length ? -1 : 1;
    }
    return 0;
}

// Buf is what backs a slice: a typed array for scalars, a plain array for everything else.
export interface Buf<T> {
    [i: number]: T;
    readonly length: number;
}

// Elem describes one element type: how to allocate a zeroed buffer, the zero value, and how to move elements.
// Struct elements move by cloning, because Go copies them by value.
export interface Elem<T> {
    alloc(n: number): Buf<T>;
    zero(): T;
    move(dst: Buf<T>, d: number, src: Buf<T>, s: number, n: number): void;
}

// stats counts the buffers the runtime allocates and their element counts.
// The bench host reads it; nothing else does.
export const stats = { count: 0, bytes: 0 };

type TypedArray = Uint8Array | Uint16Array | Uint32Array | Int32Array | Float64Array | BigInt64Array | BigUint64Array;

function typedElem<T, A extends TypedArray>(make: (n: number) => A, zero: T, width: number): Elem<T> {
    return {
        alloc(n: number): Buf<T> {
            stats.count++;
            stats.bytes += n * width;
            return make(n) as unknown as Buf<T>;
        },
        zero: () => zero,
        move(dst, d, src, s, n) {
            // The engine copies a few counters at a time far more often than it copies whole buffers.
            // A short loop beats a subarray view there, and a backward loop keeps the memmove semantics within one buffer.
            if (n <= 16) {
                if (dst === src && d > s) {
                    for (let i = n - 1; i >= 0; i--) {
                        dst[d + i] = src[s + i];
                    }
                } else {
                    for (let i = 0; i < n; i++) {
                        dst[d + i] = src[s + i];
                    }
                }
                return;
            }
            // A typed array set from an overlapping view of itself copies the source first, so this is a memmove.
            (dst as unknown as { set(a: unknown, o: number): void }).set((src as unknown as A).subarray(s, s + n), d);
        },
    };
}

function moveRefs<T>(dst: Buf<T>, d: number, src: Buf<T>, s: number, n: number): void {
    if (dst === src && d > s) {
        for (let i = n - 1; i >= 0; i--) {
            dst[d + i] = src[s + i];
        }
        return;
    }
    for (let i = 0; i < n; i++) {
        dst[d + i] = src[s + i];
    }
}

function refElem<T>(zero: T, width: number): Elem<T> {
    return {
        alloc(n: number): Buf<T> {
            stats.count++;
            stats.bytes += n * width;
            const out: T[] = new Array(n);
            for (let i = 0; i < n; i++) {
                out[i] = zero;
            }
            return out;
        },
        zero: () => zero,
        move: moveRefs,
    };
}

export const U8: Elem<number> = typedElem((n) => new Uint8Array(n), 0, 1);
export const U16: Elem<number> = typedElem((n) => new Uint16Array(n), 0, 2);
export const U32: Elem<number> = typedElem((n) => new Uint32Array(n), 0, 4);
export const I32: Elem<number> = typedElem((n) => new Int32Array(n), 0, 4);
export const INT: Elem<number> = typedElem((n) => new Float64Array(n), 0, 8);
export const I64: Elem<bigint> = typedElem((n) => new BigInt64Array(n), 0n, 8);
export const U64: Elem<bigint> = typedElem((n) => new BigUint64Array(n), 0n, 8);
export const BOOL: Elem<boolean> = refElem(false, 1);
export const STR: Elem<Str> = refElem(EMPTY, 16);

// Slice is a Go slice header: buffer, offset, length, capacity.
// Headers never change after construction; every operation that would change one returns a new header.
// Assignment therefore shares the buffer, exactly like Go.
// The nil slice has a null buffer, which the type hides because a checked index never reaches it.
export class Slice<T> {
    buf: Buf<T>;
    off: number;
    len: number;
    cap: number;

    constructor(buf: Buf<T>, off: number, len: number, cap: number) {
        this.buf = buf;
        this.off = off;
        this.len = len;
        this.cap = cap;
    }
}

export const NIL: Slice<any> = new Slice<any>(null as unknown as Buf<any>, 0, 0, 0);

export const SLICE: Elem<Slice<any>> = refElem(NIL, 32);

// structElem describes a struct element type from its zero-value constructor.
export function structElem<T extends { clone(): T }>(make: () => T, width: number): Elem<T> {
    return {
        alloc(n: number): Buf<T> {
            stats.count++;
            stats.bytes += n * width;
            const out: T[] = new Array(n);
            for (let i = 0; i < n; i++) {
                out[i] = make();
            }
            return out;
        },
        zero: make,
        move(dst, d, src, s, n) {
            if (dst === src && d > s) {
                for (let i = n - 1; i >= 0; i--) {
                    dst[d + i] = src[s + i].clone();
                }
                return;
            }
            for (let i = 0; i < n; i++) {
                dst[d + i] = src[s + i].clone();
            }
        },
    };
}

// arrayElem describes a fixed-size array element type from its zero-value constructor and its clone function.
export function arrayElem<T>(make: () => T, clone: (a: T) => T, width: number): Elem<T> {
    return {
        alloc(n: number): Buf<T> {
            stats.count++;
            stats.bytes += n * width;
            const out: T[] = new Array(n);
            for (let i = 0; i < n; i++) {
                out[i] = make();
            }
            return out;
        },
        zero: make,
        move(dst, d, src, s, n) {
            if (dst === src && d > s) {
                for (let i = n - 1; i >= 0; i--) {
                    dst[d + i] = clone(src[s + i]);
                }
                return;
            }
            for (let i = 0; i < n; i++) {
                dst[d + i] = clone(src[s + i]);
            }
        },
    };
}

export function make<T>(el: Elem<T>, n: number, c: number = n): Slice<T> {
    if (n < 0 || n > c) {
        fail("make: bad length or capacity");
    }
    return new Slice(el.alloc(c), 0, n, c);
}

export function sub<T>(s: Slice<T>, lo: number, hi: number): Slice<T> {
    if (lo < 0 || lo > hi || hi > s.cap) {
        fail("slice bounds out of range");
    }
    if (s.buf === null) {
        return s;
    }
    return new Slice(s.buf, s.off + lo, hi - lo, s.cap - lo);
}

export function head<T>(s: Slice<T>, hi: number): Slice<T> {
    return sub(s, 0, hi);
}

export function tail<T>(s: Slice<T>, lo: number): Slice<T> {
    return sub(s, lo, s.len);
}

// arrSlice views a fixed-size array, so writes through the view land in the array.
export function arrSlice<T>(arr: Buf<T>, lo: number, hi: number): Slice<T> {
    if (lo < 0 || lo > hi || hi > arr.length) {
        fail("array slice bounds out of range");
    }
    return new Slice(arr, lo, hi - lo, arr.length - lo);
}

// grow returns a header over a fresh buffer of at least need elements.
// The capacity policy is the one the Vego specification fixes: max(2 cap, 8, need).
// The spare region reads as zero, because alloc zero-fills and only the prefix is copied.
function grow<T>(el: Elem<T>, s: Slice<T>, need: number): Slice<T> {
    let newcap = s.cap * 2;
    if (newcap < 8) {
        newcap = 8;
    }
    if (newcap < need) {
        newcap = need;
    }
    const buf = el.alloc(newcap);
    if (s.len > 0) {
        el.move(buf, 0, s.buf, s.off, s.len);
    }
    return new Slice(buf, 0, s.len, newcap);
}

export function append<T>(el: Elem<T>, s: Slice<T>, v: T): Slice<T> {
    if (s.len === s.cap) {
        s = grow(el, s, s.len + 1);
    }
    s.buf[s.off + s.len] = v;
    return new Slice(s.buf, s.off, s.len + 1, s.cap);
}

// appendMany evaluates every element before the first write, for the appends where a later element reads the slice.
export function appendMany<T>(el: Elem<T>, s: Slice<T>, ...vs: T[]): Slice<T> {
    for (const v of vs) {
        s = append(el, s, v);
    }
    return s;
}

export function appendSlice<T>(el: Elem<T>, s: Slice<T>, more: Slice<T>): Slice<T> {
    if (more.len === 0) {
        return s;
    }
    if (s.len + more.len > s.cap) {
        s = grow(el, s, s.len + more.len);
    }
    el.move(s.buf, s.off + s.len, more.buf, more.off, more.len);
    return new Slice(s.buf, s.off, s.len + more.len, s.cap);
}

export function appendStr(s: Slice<number>, more: Str): Slice<number> {
    if (more.length === 0) {
        return s;
    }
    if (s.len + more.length > s.cap) {
        s = grow(U8, s, s.len + more.length);
    }
    (s.buf as Uint8Array).set(more, s.off + s.len);
    return new Slice(s.buf, s.off, s.len + more.length, s.cap);
}

export function copy<T>(el: Elem<T>, dst: Slice<T>, src: Slice<T>): number {
    const n = dst.len < src.len ? dst.len : src.len;
    if (n > 0) {
        el.move(dst.buf, dst.off, src.buf, src.off, n);
    }
    return n;
}

export function copyStr(dst: Slice<number>, src: Str): number {
    const n = dst.len < src.length ? dst.len : src.length;
    if (n > 0) {
        (dst.buf as Uint8Array).set(src.subarray(0, n), dst.off);
    }
    return n;
}

// sliceOf builds a slice from the elements of a composite literal.
export function sliceOf<T>(el: Elem<T>, vs: T[]): Slice<T> {
    const buf = el.alloc(vs.length);
    for (let i = 0; i < vs.length; i++) {
        buf[i] = vs[i];
    }
    return new Slice(buf, 0, vs.length, vs.length);
}

// strFromBytes is the string(b) conversion: a copy that nothing writes to afterwards.
export function strFromBytes(b: Slice<number>): Str {
    const out = U8.alloc(b.len) as Uint8Array;
    if (b.len > 0) {
        out.set((b.buf as Uint8Array).subarray(b.off, b.off + b.len));
    }
    return out;
}

// bytesFromStr is the []uint8(s) conversion: a fresh mutable buffer.
export function bytesFromStr(s: Str): Slice<number> {
    const buf = U8.alloc(s.length) as Uint8Array;
    buf.set(s);
    return new Slice(buf, 0, s.length, s.length);
}

// makeArr builds a fixed-size array of non-scalar elements.
export function makeArr<T>(n: number, make: () => T): T[] {
    const out: T[] = new Array(n);
    for (let i = 0; i < n; i++) {
        out[i] = make();
    }
    return out;
}

export function arrEq<T>(a: Buf<T>, b: Buf<T>, eq: (x: T, y: T) => boolean): boolean {
    for (let i = 0; i < a.length; i++) {
        if (!eq(a[i], b[i])) {
            return false;
        }
    }
    return true;
}

export function numEq(a: number | bigint | boolean, b: number | bigint | boolean): boolean {
    return a === b;
}

// The arithmetic helpers below give the Number-typed integers their Go meaning.

const MAX_EXACT = 9007199254740991;

// chk guards a 64-bit result: past 2^53 a JavaScript number is no longer an integer, so the engine stops instead.
export function chk(v: number): number {
    if (v > MAX_EXACT || v < -MAX_EXACT) {
        fail("64-bit integer result " + v + " exceeds the exact range of a JavaScript number");
    }
    return v;
}

// div and rem are Go's truncating division and remainder for number operands.
// The exactness argument: for |a| < 2^53 the rounded quotient never crosses an integer, so trunc(a / b) is the true quotient.
// The callers narrow the result for int32, where MinInt32 / -1 wraps.
export function div(a: number, b: number): number {
    if (b === 0) {
        fail("integer divide by zero");
    }
    return Math.trunc(a / b);
}

export function rem(a: number, b: number): number {
    if (b === 0) {
        fail("integer divide by zero");
    }
    // Adding zero turns a negative zero into zero.
    return (a % b) + 0;
}

export function divBig(a: bigint, b: bigint): bigint {
    if (b === 0n) {
        fail("integer divide by zero");
    }
    return a / b;
}

export function remBig(a: bigint, b: bigint): bigint {
    if (b === 0n) {
        fail("integer divide by zero");
    }
    return a % b;
}

export function shl64(a: number, n: number): number {
    if (n >= 64) {
        return 0;
    }
    return chk(a * Math.pow(2, n));
}

export function shr64(a: number, n: number): number {
    if (n >= 64) {
        return a < 0 ? -1 : 0;
    }
    return Math.floor(a / Math.pow(2, n));
}

// The 64-bit bitwise operators split a number into a high and a low half when it does not fit 32 bits.
// ToInt32 keeps the low 32 bits of any integer, and a floor division by 2^32 gives the signed high part.

function fits32(a: number, b: number): boolean {
    return (a | 0) === a && (b | 0) === b;
}

function join(hi: number, lo: number): number {
    return hi * 4294967296 + (lo >>> 0);
}

export function and64(a: number, b: number): number {
    if (fits32(a, b)) {
        return a & b;
    }
    return join(Math.floor(a / 4294967296) & Math.floor(b / 4294967296), a & b);
}

export function or64(a: number, b: number): number {
    if (fits32(a, b)) {
        return a | b;
    }
    return join(Math.floor(a / 4294967296) | Math.floor(b / 4294967296), a | b);
}

export function xor64(a: number, b: number): number {
    if (fits32(a, b)) {
        return a ^ b;
    }
    return join(Math.floor(a / 4294967296) ^ Math.floor(b / 4294967296), a ^ b);
}

export function andnot64(a: number, b: number): number {
    if (fits32(a, b)) {
        return a & ~b;
    }
    return join(Math.floor(a / 4294967296) & ~Math.floor(b / 4294967296), a & ~b);
}

export function not64(a: number): number {
    return chk(-a - 1);
}

// intOf converts a 64-bit bigint to an int number, with the wrap that Go gives an int64 to int conversion of a uint64.
export function intOf(b: bigint): number {
    const v = BigInt.asIntN(64, b);
    if (v > 9007199254740991n || v < -9007199254740991n) {
        fail("64-bit integer " + v + " exceeds the exact range of a JavaScript number");
    }
    return Number(v);
}

export function minBig(a: bigint, b: bigint): bigint {
    return a < b ? a : b;
}

export function maxBig(a: bigint, b: bigint): bigint {
    return a > b ? a : b;
}
