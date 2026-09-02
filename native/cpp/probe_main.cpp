// Probe runner for the C++ instantiation.
// It prints the same lines as dev/internal/conformance/proberef, and dev/internal/conformance/probecheck diffs them.

#include <cinttypes>
#include <cstdio>

#include "probe_engine.hpp"

static const int64_t minI64 = INT64_MIN;

static probe::Tagged mk(vg::Str a, vg::Str b, int32_t n) {
    probe::Tagged t{};
    t.Tags = {a, b};
    t.N = n;
    return t;
}

static int eq(const probe::Tagged& x, const probe::Tagged& y) {
    return probe::TaggedEq(x, y) ? 1 : 0;
}

int main() {
    vg::Arena mem;

    const int64_t pairs[5][2] = {
        {minI64, -1}, {7, -2}, {-7, 2}, {minI64, 1}, {1, minI64}};
    for (auto& ab : pairs) {
        auto qr = probe::DivMod(ab[0], ab[1]);
        std::printf("divmod %" PRId64 " %" PRId64 " = %" PRId64 " %" PRId64 "\n",
                    ab[0], ab[1], qr.r0, qr.r1);
    }
    auto qr32 = probe::DivMod32(INT32_MIN, -1);
    std::printf("divmod32 = %" PRId32 " %" PRId32 "\n", qr32.r0, qr32.r1);
    auto qr32b = probe::DivMod32(9, -4);
    std::printf("divmod32b = %" PRId32 " %" PRId32 "\n", qr32b.r0, qr32b.r1);
    std::printf("bytes = %" PRId64 " %" PRId64 "\n",
                probe::BytesProbe(mem, vg::lit("hello")), probe::BytesProbe(mem, vg::Str{}));
    probe::Counter c1{};
    std::printf("range = %" PRId64 "\n", probe::RangeProbe(mem, c1));
    std::printf("rangeval = %" PRId64 "\n",
                probe::RangeValProbe(vg::slice_of<int32_t>(mem, {3, 5, 7})));
    std::printf("rangeint = %" PRId64 "\n", probe::RangeIntProbe(5));
    std::printf("partial = %" PRId64 "\n", probe::PartialArray());
    std::printf("tagged = %d %d %d\n",
                eq(mk(vg::lit("a"), vg::lit("b"), 1), mk(vg::lit("a"), vg::lit("b"), 1)),
                eq(mk(vg::lit("a"), vg::lit("b"), 1), mk(vg::lit("a"), vg::lit("c"), 1)),
                eq(mk(vg::lit("a"), vg::lit("b"), 1), mk(vg::lit("a"), vg::lit("b"), 2)));
    probe::Counter c2{}, c3{}, c4{};
    std::printf("orderargs = %" PRId64 "\n", probe::OrderArgs(mem, c2));
    std::printf("orderbinary = %" PRId64 "\n", probe::OrderBinary(mem, c3));
    std::printf("orderindex = %" PRId64 "\n", probe::OrderIndex(mem, c4));
    std::printf("spare = %" PRId64 "\n", probe::SpareProbe(mem));
    std::printf("nil = %" PRId64 "\n", probe::NilProbe(mem));
    std::printf("wrap = %" PRId64 " %" PRId64 "\n",
                probe::WrapProbe(minI64, 3), probe::WrapProbe(7, -9));
    std::printf("narrow32 = %" PRId32 " %" PRId32 "\n",
                probe::Narrow32(INT32_MIN, -1), probe::Narrow32(-17, 5));
    std::printf("wrapu8 = %d\n", int(probe::WrapU8(3, 200)));
    std::printf("andnot = %" PRIu32 "\n", probe::AndNotProbe(0xF0F0F0F0u, 0xFF00FF00u));
    std::printf("shift = %" PRIu64 "\n", probe::ShiftProbe(0x8000000000000001ULL, 7));
    std::printf("conv = %" PRIu64 " %" PRIu64 "\n",
                probe::ConvProbe(-99), probe::ConvProbe(300));
    std::printf("subwrite = %" PRId64 "\n", probe::SubWrite(mem, 4));
    probe::Counter c5{};
    std::printf("andnotorder = %" PRId64 "\n", probe::AndNotOrder(mem, c5));
    std::printf("zeroarray = %" PRId64 "\n", probe::ZeroArray());
    std::printf("makeu64 = %" PRId64 "\n", probe::MakeU64(mem, 6));
    probe::Counter c6{};
    std::printf("pickarray = %" PRId64 "\n", probe::PickArray(mem, c6));
    return 0;
}
