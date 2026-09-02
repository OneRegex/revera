// Probe runner for the TypeScript instantiation.
// It prints the same lines as dev/internal/conformance/proberef, and dev/internal/conformance/probecheck diffs them.

import * as vg from "./vg.ts";
import * as probe from "./probe_engine.ts";

const lines: string[] = [];
const pf = (line: string) => lines.push(line);

// The constant is untyped in the Vego source, so the module holds it as a number, which is exact for a power of two.
const minI64 = BigInt(probe.minI64);

function dm(a: bigint, b: bigint): void {
    const qr = probe.DivMod(a, b);
    pf(`divmod ${a} ${b} = ${qr[0]} ${qr[1]}`);
}

dm(minI64, -1n);
dm(7n, -2n);
dm(-7n, 2n);
dm(minI64, 1n);
dm(1n, minI64);
let qr32 = probe.DivMod32(-2147483648, -1);
pf(`divmod32 = ${qr32[0]} ${qr32[1]}`);
qr32 = probe.DivMod32(9, -4);
pf(`divmod32b = ${qr32[0]} ${qr32[1]}`);
pf(`bytes = ${probe.BytesProbe(vg.lit("hello"))} ${probe.BytesProbe(vg.EMPTY)}`);
const c1 = new probe.Counter();
pf(`range = ${probe.RangeProbe(c1)}`);
pf(`rangeval = ${probe.RangeValProbe(vg.sliceOf(vg.I32, [3, 5, 7]))}`);
pf(`rangeint = ${probe.RangeIntProbe(5)}`);
pf(`partial = ${probe.PartialArray()}`);
const mk = (a: string, b: string, n: number) => new probe.Tagged([vg.lit(a), vg.lit(b)], n);
const eq = (x: probe.Tagged, y: probe.Tagged) => (probe.TaggedEq(x, y) ? 1 : 0);
pf(`tagged = ${eq(mk("a", "b", 1), mk("a", "b", 1))} ${eq(mk("a", "b", 1), mk("a", "c", 1))} ${eq(mk("a", "b", 1), mk("a", "b", 2))}`);
const c2 = new probe.Counter();
const c3 = new probe.Counter();
const c4 = new probe.Counter();
pf(`orderargs = ${probe.OrderArgs(c2)}`);
pf(`orderbinary = ${probe.OrderBinary(c3)}`);
pf(`orderindex = ${probe.OrderIndex(c4)}`);
pf(`spare = ${probe.SpareProbe()}`);
pf(`nil = ${probe.NilProbe()}`);
pf(`wrap = ${probe.WrapProbe(minI64, 3n)} ${probe.WrapProbe(7n, -9n)}`);
pf(`narrow32 = ${probe.Narrow32(-2147483648, -1)} ${probe.Narrow32(-17, 5)}`);
pf(`wrapu8 = ${probe.WrapU8(3, 200)}`);
pf(`andnot = ${probe.AndNotProbe(0xf0f0f0f0, 0xff00ff00)}`);
pf(`shift = ${probe.ShiftProbe(0x8000000000000001n, 7)}`);
pf(`conv = ${probe.ConvProbe(-99n)} ${probe.ConvProbe(300n)}`);
pf(`subwrite = ${probe.SubWrite(4)}`);
const c5 = new probe.Counter();
pf(`andnotorder = ${probe.AndNotOrder(c5)}`);
pf(`zeroarray = ${probe.ZeroArray()}`);
pf(`makeu64 = ${probe.MakeU64(6n)}`);
const c6 = new probe.Counter();
pf(`pickarray = ${probe.PickArray(c6)}`);

process.stdout.write(lines.join("\n") + "\n");
