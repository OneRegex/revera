// Probe runner for the C instantiation.
// It prints the same lines as dev/internal/conformance/proberef, and dev/internal/conformance/probecheck diffs them.

#include <inttypes.h>
#include <stdio.h>

#include "probe_engine.h"

// Generated names carry an encoded namespace, so these macros spell it once.
#define PROBE(name) vego_g_70726f6265x_##name
#define PROBE_TYPE(code) vego_t_70726f6265x_##code
#define PROBE_TUPLE(code) vego_q_70726f6265x_##code

static const int64_t minI64 = INT64_MIN;

static PROBE(Tagged) mk(vg_str a, vg_str b, int32_t n) {
    PROBE(Tagged) t = {0};
    t.vego_f_Tags.v[0] = a;
    t.vego_f_Tags.v[1] = b;
    t.vego_f_N = n;
    return t;
}

static int eq(PROBE(Tagged) x, PROBE(Tagged) y) {
    return PROBE(TaggedEq)(x, y) ? 1 : 0;
}

int main(void) {
    vg_arena mem = {0};

    const int64_t pairs[5][2] = {
        {minI64, -1}, {7, -2}, {-7, 2}, {minI64, 1}, {1, minI64}};
    for (int i = 0; i < 5; i++) {
        PROBE_TUPLE(Tup_i64_i64) qr = PROBE(DivMod)(pairs[i][0], pairs[i][1]);
        printf("divmod %" PRId64 " %" PRId64 " = %" PRId64 " %" PRId64 "\n",
               pairs[i][0], pairs[i][1], qr.r0, qr.r1);
    }
    PROBE_TUPLE(Tup_i32_i32) qr32 = PROBE(DivMod32)(INT32_MIN, -1);
    printf("divmod32 = %" PRId32 " %" PRId32 "\n", qr32.r0, qr32.r1);
    PROBE_TUPLE(Tup_i32_i32) qr32b = PROBE(DivMod32)(9, -4);
    printf("divmod32b = %" PRId32 " %" PRId32 "\n", qr32b.r0, qr32b.r1);
    printf("bytes = %" PRId64 " %" PRId64 "\n",
           PROBE(BytesProbe)(&mem, vg_lit("hello")), PROBE(BytesProbe)(&mem, (vg_str){0}));
    PROBE(Counter) c1 = {0};
    printf("range = %" PRId64 "\n", PROBE(RangeProbe)(&mem, &c1));
    printf("rangeval = %" PRId64 "\n",
           PROBE(RangeValProbe)(PROBE_TYPE(si32_of)(&mem, (const int32_t[]){3, 5, 7}, 3)));
    printf("rangeint = %" PRId64 "\n", PROBE(RangeIntProbe)(5));
    printf("partial = %" PRId64 "\n", PROBE(PartialArray)());
    printf("tagged = %d %d %d\n",
           eq(mk(vg_lit("a"), vg_lit("b"), 1), mk(vg_lit("a"), vg_lit("b"), 1)),
           eq(mk(vg_lit("a"), vg_lit("b"), 1), mk(vg_lit("a"), vg_lit("c"), 1)),
           eq(mk(vg_lit("a"), vg_lit("b"), 1), mk(vg_lit("a"), vg_lit("b"), 2)));
    PROBE(Counter) c2 = {0}, c3 = {0}, c4 = {0};
    printf("orderargs = %" PRId64 "\n", PROBE(OrderArgs)(&mem, &c2));
    printf("orderbinary = %" PRId64 "\n", PROBE(OrderBinary)(&mem, &c3));
    printf("orderindex = %" PRId64 "\n", PROBE(OrderIndex)(&mem, &c4));
    printf("spare = %" PRId64 "\n", PROBE(SpareProbe)(&mem));
    printf("nil = %" PRId64 "\n", PROBE(NilProbe)(&mem));
    printf("wrap = %" PRId64 " %" PRId64 "\n",
           PROBE(WrapProbe)(minI64, 3), PROBE(WrapProbe)(7, -9));
    printf("narrow32 = %" PRId32 " %" PRId32 "\n",
           PROBE(Narrow32)(INT32_MIN, -1), PROBE(Narrow32)(-17, 5));
    printf("wrapu8 = %d\n", (int)PROBE(WrapU8)(3, 200));
    printf("andnot = %" PRIu32 "\n", PROBE(AndNotProbe)(0xF0F0F0F0u, 0xFF00FF00u));
    printf("shift = %" PRIu64 "\n", PROBE(ShiftProbe)(0x8000000000000001ULL, 7));
    printf("conv = %" PRIu64 " %" PRIu64 "\n",
           PROBE(ConvProbe)(-99), PROBE(ConvProbe)(300));
    printf("subwrite = %" PRId64 "\n", PROBE(SubWrite)(&mem, 4));
    PROBE(Counter) c5 = {0};
    printf("andnotorder = %" PRId64 "\n", PROBE(AndNotOrder)(&mem, &c5));
    printf("zeroarray = %" PRId64 "\n", PROBE(ZeroArray)());
    printf("makeu64 = %" PRId64 "\n", PROBE(MakeU64)(&mem, 6));
    PROBE(Counter) c6 = {0};
    printf("pickarray = %" PRId64 "\n", PROBE(PickArray)(&mem, &c6));
    vg_arena_free(&mem);
    return 0;
}
