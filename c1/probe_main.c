// Probe runner for the C instantiation.
// It prints the same lines as go1/cmd/proberef, and go1/cmd/probecheck diffs them.

#include <inttypes.h>
#include <stdio.h>

#include "probe_engine.h"

static const int64_t minI64 = INT64_MIN;

static probe_Tagged mk(vg_str a, vg_str b, int32_t n) {
    probe_Tagged t = {0};
    t.Tags.v[0] = a;
    t.Tags.v[1] = b;
    t.N = n;
    return t;
}

static int eq(probe_Tagged x, probe_Tagged y) {
    return probe_TaggedEq(x, y) ? 1 : 0;
}

int main(void) {
    vg_arena mem = {0};

    const int64_t pairs[5][2] = {
        {minI64, -1}, {7, -2}, {-7, 2}, {minI64, 1}, {1, minI64}};
    for (int i = 0; i < 5; i++) {
        probe_Tup_i64_i64 qr = probe_DivMod(pairs[i][0], pairs[i][1]);
        printf("divmod %" PRId64 " %" PRId64 " = %" PRId64 " %" PRId64 "\n",
               pairs[i][0], pairs[i][1], qr.r0, qr.r1);
    }
    probe_Tup_i32_i32 qr32 = probe_DivMod32(INT32_MIN, -1);
    printf("divmod32 = %" PRId32 " %" PRId32 "\n", qr32.r0, qr32.r1);
    probe_Tup_i32_i32 qr32b = probe_DivMod32(9, -4);
    printf("divmod32b = %" PRId32 " %" PRId32 "\n", qr32b.r0, qr32b.r1);
    printf("bytes = %" PRId64 " %" PRId64 "\n",
           probe_BytesProbe(&mem, vg_lit("hello")), probe_BytesProbe(&mem, (vg_str){0}));
    probe_Counter c1 = {0};
    printf("range = %" PRId64 "\n", probe_RangeProbe(&mem, &c1));
    printf("rangeval = %" PRId64 "\n",
           probe_RangeValProbe(probe_slice_i32_of(&mem, (const int32_t[]){3, 5, 7}, 3)));
    printf("rangeint = %" PRId64 "\n", probe_RangeIntProbe(5));
    printf("partial = %" PRId64 "\n", probe_PartialArray());
    printf("tagged = %d %d %d\n",
           eq(mk(vg_lit("a"), vg_lit("b"), 1), mk(vg_lit("a"), vg_lit("b"), 1)),
           eq(mk(vg_lit("a"), vg_lit("b"), 1), mk(vg_lit("a"), vg_lit("c"), 1)),
           eq(mk(vg_lit("a"), vg_lit("b"), 1), mk(vg_lit("a"), vg_lit("b"), 2)));
    probe_Counter c2 = {0}, c3 = {0}, c4 = {0};
    printf("orderargs = %" PRId64 "\n", probe_OrderArgs(&mem, &c2));
    printf("orderbinary = %" PRId64 "\n", probe_OrderBinary(&mem, &c3));
    printf("orderindex = %" PRId64 "\n", probe_OrderIndex(&mem, &c4));
    printf("spare = %" PRId64 "\n", probe_SpareProbe(&mem));
    printf("nil = %" PRId64 "\n", probe_NilProbe(&mem));
    printf("wrap = %" PRId64 " %" PRId64 "\n",
           probe_WrapProbe(minI64, 3), probe_WrapProbe(7, -9));
    printf("narrow32 = %" PRId32 " %" PRId32 "\n",
           probe_Narrow32(INT32_MIN, -1), probe_Narrow32(-17, 5));
    printf("wrapu8 = %d\n", (int)probe_WrapU8(3, 200));
    printf("andnot = %" PRIu32 "\n", probe_AndNotProbe(0xF0F0F0F0u, 0xFF00FF00u));
    printf("shift = %" PRIu64 "\n", probe_ShiftProbe(0x8000000000000001ULL, 7));
    printf("conv = %" PRIu64 " %" PRIu64 "\n",
           probe_ConvProbe(-99), probe_ConvProbe(300));
    printf("subwrite = %" PRId64 "\n", probe_SubWrite(&mem, 4));
    probe_Counter c5 = {0};
    printf("andnotorder = %" PRId64 "\n", probe_AndNotOrder(&mem, &c5));
    printf("zeroarray = %" PRId64 "\n", probe_ZeroArray());
    printf("makeu64 = %" PRId64 "\n", probe_MakeU64(&mem, 6));
    probe_Counter c6 = {0};
    printf("pickarray = %" PRId64 "\n", probe_PickArray(&mem, &c6));
    vg_arena_free(&mem);
    return 0;
}
