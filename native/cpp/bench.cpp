// Benchmark host for the C++ instantiation of the revera engine.
// It reads bench protocol commands on stdin, one per line, and prints one answer line per command.
// dev/internal/protocol/bench.go, the Go reference implementation, defines the protocol.
//
// The host owns four arenas.
// Locale data lives in the persistent arena.
// The decoded strings of one B command live in the input arena, which resets at the start of each B command.
// A compile iteration resets the pattern arena, and a match or replace iteration resets the scratch arena.
// The untimed pass reads the allocation counters of those two arenas.
// The timed passes read a monotonic clock around a whole pass.

#include <chrono>
#include <cinttypes>
#include <cstdio>
#include <cstdlib>
#include <string>

#include "host.hpp"

using namespace revera::engine;

enum class Kind { Compile, Match, Replace };

// Case holds the inputs of one B command.
// The compile kind reads pat, loc and cflags, and the other kinds read re, subject and repl.
struct Case {
    Kind kind = Kind::Compile;
    vg::Str pat;
    Locale loc;
    uint32_t cflags = 0;
    vg::Str subject;
    vg::Str repl;
    Regexp re;
};

// A B command has a fixed shape, so a missing token is a protocol error.
static char* tok_need(char** cursor) {
    char* t = tok_next(cursor);
    if (t == nullptr) {
        std::fputs("malformed bench command\n", stderr);
        std::exit(1);
    }
    return t;
}

// run_pass performs iters operations of the kind the case names.
// Each kind is a plain loop, so nothing but the engine call sits inside the timed region.
static void run_pass(Case& c, int64_t iters, vg::Arena& pattern, vg::Arena& scratch) {
    switch (c.kind) {
    case Kind::Compile:
        for (int64_t i = 0; i < iters; i++) {
            pattern.reset();
            Compile(pattern, c.pat, c.loc, c.cflags);
        }
        break;
    case Kind::Match: {
        int64_t nmatch = NumSub(c.re) + 1;
        for (int64_t i = 0; i < iters; i++) {
            scratch.reset();
            auto pmatch = vg::make<Match>(scratch, nmatch);
            Exec(scratch, c.re, c.subject, pmatch, 0);
        }
        break;
    }
    case Kind::Replace:
        for (int64_t i = 0; i < iters; i++) {
            scratch.reset();
            ReplaceAll(scratch, c.re, c.subject, c.repl, -1, 0);
        }
        break;
    }
}

int main() {
    vg::Arena persistent;
    vg::Arena input;
    vg::Arena pattern;
    vg::Arena scratch;

    Locale base_loc = base_locale();
    Locale cur_loc = LocalePOSIX();

    std::string line;
    while (read_line(line)) {
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
            vg::Str name = decode(persistent, tok_need(&cursor));
            vg::Str coll = decode(persistent, tok_need(&cursor));
            auto res = LocaleSelect(persistent, base_loc, name, coll);
            if (res.r1) {
                cur_loc = res.r0;
            }
            std::printf("L %d\n", res.r1 ? 1 : 0);
            break;
        }
        case 'B': {
            std::string name = tok_need(&cursor);
            std::string kind_tok = tok_need(&cursor);
            int64_t iters = std::strtoll(tok_need(&cursor), nullptr, 10);
            int64_t reps = std::strtoll(tok_need(&cursor), nullptr, 10);
            Case c;
            c.cflags = uint32_t(std::strtoul(tok_need(&cursor), nullptr, 10));
            input.reset();
            c.pat = decode(input, tok_need(&cursor));
            c.subject = decode(input, tok_need(&cursor));
            c.repl = decode(input, tok_need(&cursor));
            c.loc = cur_loc;
            if (kind_tok == "compile") {
                c.kind = Kind::Compile;
            } else if (kind_tok == "match") {
                c.kind = Kind::Match;
            } else if (kind_tok == "replace") {
                c.kind = Kind::Replace;
            } else {
                std::fprintf(stderr, "unknown bench kind %s\n", kind_tok.c_str());
                return 1;
            }
            if (iters <= 0) {
                std::fputs("bench iters must be positive\n", stderr);
                return 1;
            }
            pattern.reset();
            scratch.reset();
            auto res = Compile(pattern, c.pat, c.loc, c.cflags);
            if (res.r1.Code != 0) {
                std::printf("B %s %" PRId32 " 0 0\n", name.c_str(), res.r1.Code);
                break;
            }
            c.re = res.r0;

            uint64_t bytes0 = pattern.alloc_bytes() + scratch.alloc_bytes();
            uint64_t allocs0 = pattern.alloc_count() + scratch.alloc_count();
            run_pass(c, iters, pattern, scratch);
            uint64_t bytes = (pattern.alloc_bytes() + scratch.alloc_bytes() - bytes0) / uint64_t(iters);
            uint64_t allocs = (pattern.alloc_count() + scratch.alloc_count() - allocs0) / uint64_t(iters);
            std::printf("B %s 0 %" PRIu64 " %" PRIu64, name.c_str(), bytes, allocs);

            for (int64_t r = 0; r < reps; r++) {
                auto start = std::chrono::steady_clock::now();
                run_pass(c, iters, pattern, scratch);
                auto elapsed = std::chrono::steady_clock::now() - start;
                int64_t ns = std::chrono::duration_cast<std::chrono::nanoseconds>(elapsed).count();
                std::printf(" %" PRId64, ns);
            }
            std::putchar('\n');
            break;
        }
        default:
            std::fputs("unknown bench command\n", stderr);
            return 1;
        }
        std::fflush(stdout);
    }
    return 0;
}
