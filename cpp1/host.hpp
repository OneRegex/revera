// Pieces that the driver, bench and fuzz hosts share.
// The locale blob is embedded once here, and base_locale() decodes it once per process.
// The hex and token helpers serve the line protocols of the driver and the bench.
// Everything is static, so each host gets its own copy and the header needs no source file.

#pragma once

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>

#include "engine.hpp"

static const char data_bin[] = {
#embed "data.bin"
};

// LocaleLoad validates the whole blob, so the result is worth keeping for the life of the process.
static const revera::engine::Locale& base_locale() {
    static const revera::engine::Locale base = [] {
        auto loaded = revera::engine::LocaleLoad(vg::Str{data_bin, int64_t(sizeof(data_bin))});
        if (!loaded.r1) {
            std::fputs("embedded locale data failed to load\n", stderr);
            std::abort();
        }
        return loaded.r0;
    }();
    return base;
}

// The fuzz host reads packed binary inputs and not lines, so it leaves the helpers below unused.
[[maybe_unused]] static uint8_t hexval(char c) {
    if (c >= '0' && c <= '9') {
        return uint8_t(c - '0');
    }
    return uint8_t(c - 'a' + 10);
}

[[maybe_unused]] static vg::Str decode(vg::Arena& mem, const std::string& tok) {
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

// Tokens of the current line, split on single spaces.
// The first call hands strtok the line, and later calls continue it.
[[maybe_unused]] static char* tok_next(char** cursor) {
    char* t = std::strtok(*cursor, " \n");
    *cursor = nullptr;
    return t;
}
