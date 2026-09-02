// Fuzz entry point for the C++ instantiation of the revera engine.
// dev/internal/protocol/fuzz.go, the Go reference, defines the input format that every target shares.
// LLVMFuzzerTestOneInput serves libFuzzer.
// With REVERA_FUZZ_STANDALONE defined, a main function replays a pack of recorded inputs instead.
//
// The property is freedom from crashes and from sanitizer reports.
// Every result is ignored.
//
// Input format:
//
//   byte 0        compile flags, masked with 0x0f
//   byte 1        bits 0 and 1 are the exec flags, bit 4 selects locale "cs", else bit 5 selects locale "tr"
//   byte 2        n, the pattern length
//   bytes 3..     the pattern, n bytes or fewer if the input ends early
//   next byte     m, the replacement length
//   next m bytes  the replacement, fewer if the input ends early
//   rest          the subject

#include <cinttypes>
#include <cstddef>
#include <cstdint>
#include <cstdio>
#include <fstream>
#include <iterator>
#include <vector>

#include "host.hpp"

using namespace revera::engine;

// take views the next n bytes of the input, or fewer if the input ends early.
static vg::Str take(const uint8_t* data, size_t size, size_t& pos, size_t n) {
    if (n > size - pos) {
        n = size - pos;
    }
    vg::Str s{reinterpret_cast<const char*>(data + pos), int64_t(n)};
    pos += n;
    return s;
}

static void fuzz_one(const uint8_t* data, size_t size) {
    if (size < 3) {
        return;
    }
    uint32_t cflags = data[0] & 0x0f;
    uint32_t eflags = data[1] & 0x03;
    size_t pos = 3;
    vg::Str pattern = take(data, size, pos, data[2]);
    vg::Str replacement;
    if (pos < size) {
        size_t m = data[pos];
        pos++;
        replacement = take(data, size, pos, m);
    }
    vg::Str subject = take(data, size, pos, size - pos);

    // One arena serves the whole input and frees everything when it goes out of scope.
    vg::Arena mem;
    Locale loc = LocalePOSIX();
    if (data[1] & 0x30) {
        vg::Str name = (data[1] & 0x10) ? vg::lit("cs") : vg::lit("tr");
        Locale base = base_locale();
        auto sel = LocaleSelect(mem, base, name, vg::Str{});
        if (!sel.r1) {
            return;
        }
        loc = sel.r0;
    }
    auto compiled = Compile(mem, pattern, loc, cflags);
    if (compiled.r1.Code != 0) {
        return;
    }
    Regexp re = compiled.r0;
    auto pmatch = vg::make<Match>(mem, NumSub(re) + 1);
    Exec(mem, re, subject, pmatch, eflags);
    ReplaceAll(mem, re, subject, replacement, -1, eflags);
    auto ires = MatchIterInit(re, 3);
    if (ires.r1.Code == 0) {
        MatchIter iter = ires.r0;
        for (;;) {
            auto next = MatchIterNext(mem, re, iter, subject, eflags, pmatch);
            if (next.r1.Code != 0 || !next.r0) {
                break;
            }
        }
    }
    Contract c = ContractFor(re, subject.len);
    ContractHeapBytes(c);
    ContractStackBytes(c);
    ContractSteps(c);
}

extern "C" int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
    fuzz_one(data, size);
    return 0;
}

#ifdef REVERA_FUZZ_STANDALONE

// The pack is a sequence of records.
// Each record is a 4-byte little-endian length followed by that many bytes.
// The whole pack is read first, so a corrupt length never turns into a huge allocation.
int main(int argc, char** argv) {
    if (argc != 2) {
        std::fputs("usage: fuzzcase <packfile>\n", stderr);
        return 1;
    }
    std::ifstream f(argv[1], std::ios::binary);
    if (!f) {
        std::perror(argv[1]);
        return 1;
    }
    std::vector<uint8_t> pack((std::istreambuf_iterator<char>(f)), std::istreambuf_iterator<char>());

    size_t pos = 0;
    uint64_t count = 0;
    while (pos < pack.size()) {
        if (pack.size() - pos < 4) {
            std::fprintf(stderr, "%s: truncated record header after %" PRIu64 " inputs\n", argv[1], count);
            return 1;
        }
        uint32_t n = uint32_t(pack[pos]) | uint32_t(pack[pos + 1]) << 8 | uint32_t(pack[pos + 2]) << 16 |
                     uint32_t(pack[pos + 3]) << 24;
        pos += 4;
        if (n > pack.size() - pos) {
            std::fprintf(stderr, "%s: truncated record after %" PRIu64 " inputs\n", argv[1], count);
            return 1;
        }
        fuzz_one(pack.data() + pos, n);
        pos += n;
        count++;
    }
    std::printf("fuzzcase: %" PRIu64 " inputs\n", count);
    return 0;
}

#endif
