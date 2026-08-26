// Differential driver for the C++ instantiation of the revera
// engine. It reads protocol commands on stdin, one per line, and
// prints one output line per command. The protocol is defined by
// go1/revera/driver_host.go, the Go reference implementation.
//
// The host owns three arenas. Locale data lives in the persistent
// arena. A compiled pattern lives in the pattern arena until the
// next compile. Everything one operation allocates comes from the
// scratch arena, reset before each operation. Each engine call
// receives the arena that must back its allocations.

#include <cinttypes>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

#include "engine.hpp"

static const char data_bin[] = {
#embed "data.bin"
};

using namespace revera;

static uint8_t hexval(char c) {
    if (c >= '0' && c <= '9') {
        return uint8_t(c - '0');
    }
    return uint8_t(c - 'a' + 10);
}

static vg::Str decode(vg::Arena& mem, const std::string& tok) {
    if (tok == "-") {
        return vg::Str{};
    }
    int64_t n = int64_t(tok.size() / 2);
    vg::Slice<uint8_t> buf = vg::make<uint8_t>(mem, n);
    for (int64_t i = 0; i < n; i++) {
        buf.p[i] = uint8_t(hexval(tok[2 * i]) << 4 | hexval(tok[2 * i + 1]));
    }
    return vg::Str{reinterpret_cast<const char*>(buf.p), n};
}

static void print_hex(vg::Str s) {
    if (s.len == 0) {
        std::fputs("-", stdout);
        return;
    }
    for (int64_t i = 0; i < s.len; i++) {
        std::printf("%02x", unsigned(s[i]));
    }
}

// Tokens of the current line, split on single spaces. The first
// call hands strtok the line; later calls continue it.
static char* tok_next(char** cursor) {
    char* t = std::strtok(*cursor, " \n");
    *cursor = nullptr;
    return t;
}

int main() {
    vg::Arena persistent;
    vg::Arena pattern;
    vg::Arena scratch;

    Locale base_loc;
    Locale cur_loc;
    Regexp cur_re;
    bool re_valid = false;

    auto loaded = LocaleLoad(vg::Str{data_bin, int64_t(sizeof(data_bin))});
    if (!loaded.r1) {
        std::fputs("embedded locale data failed to load\n", stderr);
        return 1;
    }
    base_loc = loaded.r0;
    cur_loc = LocalePOSIX();

    std::vector<char> line(1 << 20);
    while (std::fgets(line.data(), int(line.size()), stdin) != nullptr) {
        char* cursor = line.data();
        char* cmd = tok_next(&cursor);
        if (cmd == nullptr) {
            continue;
        }
        switch (cmd[0]) {
        case 'P': {
            cur_loc = LocalePOSIX();
            std::puts("P 1");
            break;
        }
        case 'L': {
            vg::Str name = decode(persistent, tok_next(&cursor));
            vg::Str coll = decode(persistent, tok_next(&cursor));
            auto res = LocaleSelect(persistent, base_loc, name, coll);
            if (res.r1) {
                cur_loc = res.r0;
            }
            std::printf("L %d\n", res.r1 ? 1 : 0);
            break;
        }
        case 'C': {
            uint32_t flags = uint32_t(std::strtoul(tok_next(&cursor), nullptr, 10));
            std::string patTok = tok_next(&cursor);
            re_valid = false;
            cur_re = Regexp{};
            pattern.reset();
            scratch.reset();
            vg::Str pat = decode(pattern, patTok);
            auto res = Compile(pattern, pat, cur_loc, flags);
            if (res.r1.Code != 0) {
                std::printf("C %" PRId32 " %" PRId64 " 0\n", res.r1.Code, res.r1.Pos);
                break;
            }
            cur_re = res.r0;
            re_valid = true;
            std::printf("C 0 0 %" PRId64 "\n", NumSub(cur_re));
            break;
        }
        case 'X': {
            if (!re_valid) {
                std::puts("X ERR");
                break;
            }
            scratch.reset();
            uint32_t eflags = uint32_t(std::strtoul(tok_next(&cursor), nullptr, 10));
            vg::Str subject = decode(scratch, tok_next(&cursor));
            auto pmatch = vg::make<Match>(scratch, NumSub(cur_re) + 1);
            auto res = Exec(scratch, cur_re, subject, pmatch, eflags);
            if (res.r1.Code != 0) {
                std::printf("X %" PRId32 " 0\n", res.r1.Code);
                break;
            }
            if (!res.r0) {
                std::puts("X 0 0");
                break;
            }
            std::fputs("X 0 1", stdout);
            for (int64_t i = 0; i < pmatch.len; i++) {
                std::printf(" %" PRId64 ",%" PRId64, pmatch[i].So, pmatch[i].Eo);
            }
            std::putchar('\n');
            break;
        }
        case 'R': {
            if (!re_valid) {
                std::puts("R ERR");
                break;
            }
            scratch.reset();
            int64_t limit = std::strtoll(tok_next(&cursor), nullptr, 10);
            uint32_t eflags = uint32_t(std::strtoul(tok_next(&cursor), nullptr, 10));
            vg::Str repl = decode(scratch, tok_next(&cursor));
            vg::Str subject = decode(scratch, tok_next(&cursor));
            auto res = ReplaceAll(scratch, cur_re, subject, repl, limit, eflags);
            if (res.r1.Code != 0) {
                std::printf("R %" PRId32 " %lld -\n", res.r1.Code, (long long)res.r1.Pos);
                break;
            }
            std::fputs("R 0 0 ", stdout);
            print_hex(res.r0);
            std::putchar('\n');
            break;
        }
        case 'I': {
            if (!re_valid) {
                std::puts("I ERR");
                break;
            }
            scratch.reset();
            int64_t limit = std::strtoll(tok_next(&cursor), nullptr, 10);
            uint32_t eflags = uint32_t(std::strtoul(tok_next(&cursor), nullptr, 10));
            vg::Str subject = decode(scratch, tok_next(&cursor));
            auto ires = MatchIterInit(cur_re, limit);
            if (ires.r1.Code != 0) {
                std::printf("I %" PRId32 " 0\n", ires.r1.Code);
                break;
            }
            MatchIter iter = ires.r0;
            auto pmatch = vg::make<Match>(scratch, NumSub(cur_re) + 1);
            std::string rows;
            int64_t count = 0;
            bool failed = false;
            for (;;) {
                auto res = MatchIterNext(scratch, cur_re, iter, subject, eflags, pmatch);
                if (res.r1.Code != 0) {
                    std::printf("I %" PRId32 " 0\n", res.r1.Code);
                    failed = true;
                    break;
                }
                if (!res.r0) {
                    break;
                }
                if (count > 0) {
                    rows += '|';
                }
                char buf[64];
                for (int64_t i = 0; i < pmatch.len; i++) {
                    std::snprintf(buf, sizeof(buf), "%s%" PRId64 ",%" PRId64,
                                  i > 0 ? "," : "", pmatch[i].So, pmatch[i].Eo);
                    rows += buf;
                }
                count++;
            }
            if (failed) {
                break;
            }
            std::printf("I 0 %" PRId64, count);
            if (count > 0) {
                std::printf(" %s", rows.c_str());
            }
            std::putchar('\n');
            break;
        }
        case 'T': {
            if (!re_valid) {
                std::puts("T ERR");
                break;
            }
            scratch.reset();
            int64_t maxInput = std::strtoll(tok_next(&cursor), nullptr, 10);
            Contract c = ContractFor(cur_re, maxInput);
            std::printf("T %d %" PRId64 " %" PRId64 " %" PRId64 "\n",
                        c.HasSolver ? 1 : 0, ContractHeapBytes(c),
                        ContractStackBytes(c), ContractSteps(c));
            break;
        }
        case 'O': {
            int64_t lo = std::strtoll(tok_next(&cursor), nullptr, 10);
            int64_t hi = std::strtoll(tok_next(&cursor), nullptr, 10);
            uint64_t h = 0xcbf29ce484222325ULL;
            int32_t hi32 = int32_t(hi);
            for (int32_t r = int32_t(lo); r < hi32; r++) {
                h ^= uint64_t(uint32_t(localeToUpper(cur_loc, r)));
                h *= 0x100000001b3ULL;
                h ^= uint64_t(uint32_t(localeToLower(cur_loc, r)));
                h *= 0x100000001b3ULL;
            }
            std::printf("O %" PRIu64 "\n", h);
            break;
        }
        default:
            std::fputs("unknown driver command\n", stderr);
            return 1;
        }
        std::fflush(stdout);
    }
    return 0;
}
