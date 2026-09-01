// Pieces that the driver, bench and fuzz hosts share.
// The locale blob is embedded once here, and base_locale() decodes it once per process.
// The hex and token helpers serve the line protocols of the driver and the bench.
// Everything is static inline, so each host gets its own copy and the header needs no source file.

#ifndef REVERA_HOST_H
#define REVERA_HOST_H

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "engine.h"

static const char data_bin[] = {
#embed "data.bin"
};

// LocaleLoad validates the whole blob, so the result is worth keeping for the life of the process.
// The hosts are single threaded, so a plain static flag is enough for the lazy init.
static inline revera_eng_Locale base_locale(void) {
    static bool loaded = false;
    static revera_eng_Locale base;
    if (!loaded) {
        revera_eng_Tup_t4c6f63616c65x_bool res =
            revera_eng_LocaleLoad((vg_str){data_bin, (int64_t)sizeof(data_bin)});
        if (!res.r1) {
            fputs("embedded locale data failed to load\n", stderr);
            abort();
        }
        base = res.r0;
        loaded = true;
    }
    return base;
}

// The fuzz host reads packed binary inputs and not lines, so it leaves the helpers below unused.
static inline uint8_t hexval(char c) {
    if (c >= '0' && c <= '9') {
        return (uint8_t)(c - '0');
    }
    return (uint8_t)(c - 'a' + 10);
}

static inline vg_str decode(vg_arena *mem, const char *tok) {
    if (strcmp(tok, "-") == 0) {
        return (vg_str){0};
    }
    int64_t n = (int64_t)(strlen(tok) / 2);
    revera_eng_slice_u8 buf = revera_eng_slice_u8_make(mem, n);
    for (int64_t i = 0; i < n; i++) {
        buf.p[i] = (uint8_t)(hexval(tok[2 * i]) << 4 | hexval(tok[2 * i + 1]));
    }
    return (vg_str){(const char *)buf.p, n};
}

// Tokens of the current line, split on single spaces.
// The first call hands strtok the line, and later calls continue it.
static inline char *tok_next(char **cursor) {
    char *t = strtok(*cursor, " \n");
    *cursor = NULL;
    return t;
}

#endif
