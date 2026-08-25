package probe

// Host file, outside the Vego subset. ReportLines runs the probe
// call matrix and formats one line per result. The Zig, C++ and
// Rust probe binaries print the same lines.

import "fmt"

func ReportLines() []string {
	var out []string
	pf := func(format string, args ...any) {
		out = append(out, fmt.Sprintf(format, args...))
	}
	dm := func(a, b int64) {
		q, r := DivMod(a, b)
		pf("divmod %d %d = %d %d", a, b, q, r)
	}
	dm(minI64, -1)
	dm(7, -2)
	dm(-7, 2)
	dm(minI64, 1)
	dm(1, minI64)
	q32, r32 := DivMod32(-2147483648, -1)
	pf("divmod32 = %d %d", q32, r32)
	q32, r32 = DivMod32(9, -4)
	pf("divmod32b = %d %d", q32, r32)
	pf("bytes = %d %d", BytesProbe("hello"), BytesProbe(""))
	var c1 Counter
	pf("range = %d", RangeProbe(&c1))
	pf("rangeval = %d", RangeValProbe([]int32{3, 5, 7}))
	pf("rangeint = %d", RangeIntProbe(5))
	pf("partial = %d", PartialArray())
	mk := func(a, b string, n int32) Tagged {
		return Tagged{Tags: [2]string{a, b}, N: n}
	}
	eq := func(x, y Tagged) int {
		if TaggedEq(x, y) {
			return 1
		}
		return 0
	}
	pf("tagged = %d %d %d",
		eq(mk("a", "b", 1), mk("a", "b", 1)),
		eq(mk("a", "b", 1), mk("a", "c", 1)),
		eq(mk("a", "b", 1), mk("a", "b", 2)))
	var c2, c3, c4 Counter
	pf("orderargs = %d", OrderArgs(&c2))
	pf("orderbinary = %d", OrderBinary(&c3))
	pf("orderindex = %d", OrderIndex(&c4))
	pf("spare = %d", SpareProbe())
	pf("nil = %d", NilProbe())
	pf("wrap = %d %d", WrapProbe(minI64, 3), WrapProbe(7, -9))
	pf("narrow32 = %d %d", Narrow32(-2147483648, -1), Narrow32(-17, 5))
	pf("wrapu8 = %d", WrapU8(3, 200))
	pf("andnot = %d", AndNotProbe(0xF0F0F0F0, 0xFF00FF00))
	pf("shift = %d", ShiftProbe(0x8000000000000001, 7))
	pf("conv = %d %d", ConvProbe(-99), ConvProbe(300))
	pf("subwrite = %d", SubWrite(4))
	var c5 Counter
	pf("andnotorder = %d", AndNotOrder(&c5))
	pf("zeroarray = %d", ZeroArray())
	pf("makeu64 = %d", MakeU64(6))
	var c6 Counter
	pf("pickarray = %d", PickArray(&c6))
	return out
}
