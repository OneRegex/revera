import { test } from "node:test";
import assert from "node:assert/strict";

import * as vg from "../src/vg.ts";

test("64-bit arithmetic stays exact or throws", () => {
    assert.equal(vg.chk(2 ** 53 - 1), 2 ** 53 - 1);
    assert.throws(() => vg.chk(2 ** 53), RangeError);
    assert.throws(() => vg.chk(-(2 ** 53)), RangeError);

    assert.equal(vg.div(-7, 2), -3);
    assert.equal(vg.rem(-7, 2), -1);
    assert.ok(Object.is(vg.rem(-4, 2), 0));
    assert.throws(() => vg.div(1, 0), RangeError);
    // A power of two past 2^53 is an exact double, but dividing it would round.
    assert.throws(() => vg.div(2 ** 62, 3), RangeError);
    assert.throws(() => vg.rem(2 ** 62, 3), RangeError);

    assert.equal(vg.shl64(1, 40), 2 ** 40);
    assert.equal(vg.shl64(1, 64), 0);
    assert.throws(() => vg.shl64(1, 60), RangeError);
    assert.throws(() => vg.shl64(1, -1), RangeError);
    assert.equal(vg.shr64(-5, 1), -3);
    assert.equal(vg.shr64(2 ** 62, 2), 2 ** 60);
    assert.equal(vg.shr64(-1, 64), -1);
    assert.throws(() => vg.shr64(1, -1), RangeError);

    assert.equal(vg.and64(2 ** 40 + 5, 7), 5);
    assert.equal(vg.and64(-1, 2 ** 40), 2 ** 40);
    assert.equal(vg.or64(2 ** 40, 1), 2 ** 40 + 1);
    assert.equal(vg.xor64(-1, 2 ** 40), -(2 ** 40) - 1);
    assert.equal(vg.andnot64(2 ** 40 + 5, 4), 2 ** 40 + 1);
    assert.throws(() => vg.and64(2 ** 62, 7), RangeError);
    assert.equal(vg.not64(5), -6);

    assert.equal(vg.intOf(BigInt.asUintN(64, -1n)), -1);
    assert.throws(() => vg.intOf(1n << 60n), RangeError);
    assert.equal(vg.divBig(-(1n << 63n), -1n), 1n << 63n);
    assert.throws(() => vg.divBig(1n, 0n), RangeError);
});

test("slices follow the Go model", () => {
    const nil: vg.Slice<number> = vg.NIL;
    assert.equal(nil.buf, null);
    assert.equal(vg.sub(nil, 0, 0).buf, null);
    assert.equal(vg.appendSlice(vg.I32, nil, vg.make(vg.I32, 0)).buf, null);

    let s = vg.make(vg.I32, 0, 2);
    s = vg.append(vg.I32, s, 1);
    s = vg.append(vg.I32, s, 2);
    const before = s;
    s = vg.append(vg.I32, s, 3);
    assert.equal(before.cap, 2);
    assert.equal(s.cap, 8);
    assert.equal(s.len, 3);
    assert.equal(vg.head(s, 4).buf[3], 0);

    // A view shares the buffer, so a write through it lands in the owner.
    const view = vg.sub(s, 1, 3);
    view.buf[view.off] = 9;
    assert.equal(s.buf[1], 9);
    assert.equal(vg.copy(vg.I32, vg.sub(s, 0, 2), vg.sub(s, 1, 3)), 2);
    assert.deepEqual([s.buf[0], s.buf[1], s.buf[2]], [9, 3, 3]);
    assert.throws(() => vg.ix(3, 3), RangeError);
    assert.throws(() => vg.sub(s, 2, 1), RangeError);
});

test("strings are byte views", () => {
    const hello = vg.lit("hello");
    assert.equal(vg.strSub(hello, 1, 3).length, 2);
    assert.ok(vg.streq(vg.strSub(hello, 1, 3), vg.lit("el")));
    assert.equal(vg.strcmp3(vg.lit("ab"), vg.lit("abc")), -1);
    assert.equal(vg.strcmp3(vg.lit("b"), vg.lit("abc")), 1);
    assert.throws(() => vg.strSub(hello, 4, 9), RangeError);

    const bytes = vg.bytesFromStr(hello);
    bytes.buf[0] = 0x48;
    assert.equal(hello[0], 0x68);
    assert.ok(vg.streq(vg.strFromBytes(bytes), vg.lit("Hello")));
    assert.equal(vg.appendStr(vg.NIL, vg.EMPTY).buf, null);
    assert.ok(vg.streq(vg.strFromBytes(vg.appendStr(vg.NIL, hello)), hello));
});
