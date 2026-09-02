// Fuzz entry point for the C11 instantiation of the revera engine.
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

#include <inttypes.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#include "host.h"

// take views the next n bytes of the input, or fewer if the input ends early.
static vg_str take(const uint8_t *data, size_t size, size_t *pos, size_t n) {
    if (n > size - *pos) {
        n = size - *pos;
    }
    vg_str s = {(const char *)(data + *pos), (int64_t)n};
    *pos += n;
    return s;
}

static void fuzz_one(const uint8_t *data, size_t size) {
    if (size < 3) {
        return;
    }
    uint32_t cflags = data[0] & 0x0f;
    uint32_t eflags = data[1] & 0x03;
    size_t pos = 3;
    vg_str pattern = take(data, size, &pos, data[2]);
    vg_str replacement = {0};
    if (pos < size) {
        size_t n = data[pos++];
        replacement = take(data, size, &pos, n);
    }
    vg_str subject = take(data, size, &pos, size - pos);

    // One arena serves the whole input and frees everything when the call ends.
    vg_arena mem = {0};
    revera_eng_Locale loc = revera_eng_LocalePOSIX();
    if (data[1] & 0x30) {
        vg_str name = (data[1] & 0x10) ? vg_lit("cs") : vg_lit("tr");
        revera_eng_Locale base = base_locale();
        revera_eng_Tup_t4c6f63616c65x_bool sel =
            revera_eng_LocaleSelect(&mem, &base, name, (vg_str){0});
        if (!sel.r1) {
            vg_arena_free(&mem);
            return;
        }
        loc = sel.r0;
    }
    revera_eng_Tup_t526567657870x_t4572726f72x compiled =
        revera_eng_Compile(&mem, pattern, loc, cflags);
    if (compiled.r1.Code != 0) {
        vg_arena_free(&mem);
        return;
    }
    revera_eng_Regexp re = compiled.r0;
    revera_eng_slice_Match pmatch =
        revera_eng_slice_Match_make(&mem, revera_eng_NumSub(&re) + 1);
    (void)revera_eng_Exec(&mem, &re, subject, pmatch, eflags);
    (void)revera_eng_ReplaceAll(&mem, &re, subject, replacement, -1, eflags);
    revera_eng_Tup_t4d6174636849746572x_t4572726f72x init =
        revera_eng_MatchIterInit(&re, 3);
    if (init.r1.Code == 0) {
        revera_eng_MatchIter iter = init.r0;
        for (;;) {
            revera_eng_Tup_bool_t4572726f72x next =
                revera_eng_MatchIterNext(&mem, &re, &iter, subject, eflags, pmatch);
            if (next.r1.Code != 0 || !next.r0) {
                break;
            }
        }
    }
    revera_eng_Contract c = revera_eng_ContractFor(&re, subject.len);
    (void)revera_eng_ContractHeapBytes(&c);
    (void)revera_eng_ContractStackBytes(&c);
    (void)revera_eng_ContractSteps(&c);
    vg_arena_free(&mem);
}

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    fuzz_one(data, size);
    return 0;
}

#ifdef REVERA_FUZZ_STANDALONE

// The pack is a sequence of records.
// Each record is a 4-byte little-endian length followed by that many bytes.
// The whole pack is read first, so a corrupt length never turns into a huge allocation.
int main(int argc, char **argv) {
    if (argc != 2) {
        fputs("usage: fuzzcase <packfile>\n", stderr);
        return 1;
    }
    FILE *file = fopen(argv[1], "rb");
    if (file == NULL) {
        perror(argv[1]);
        return 1;
    }
    if (fseek(file, 0, SEEK_END) != 0) {
        perror(argv[1]);
        fclose(file);
        return 1;
    }
    long end = ftell(file);
    if (end < 0 || fseek(file, 0, SEEK_SET) != 0) {
        perror(argv[1]);
        fclose(file);
        return 1;
    }
    size_t size = (size_t)end;
    uint8_t *pack = (uint8_t *)malloc(size ? size : 1);
    if (pack == NULL) {
        abort();
    }
    if (fread(pack, 1, size, file) != size) {
        perror(argv[1]);
        free(pack);
        fclose(file);
        return 1;
    }
    fclose(file);

    size_t pos = 0;
    uint64_t count = 0;
    while (pos < size) {
        if (size - pos < 4) {
            fprintf(stderr, "%s: truncated record header after %" PRIu64 " inputs\n", argv[1], count);
            free(pack);
            return 1;
        }
        uint32_t n = (uint32_t)pack[pos] | (uint32_t)pack[pos + 1] << 8 |
                     (uint32_t)pack[pos + 2] << 16 | (uint32_t)pack[pos + 3] << 24;
        pos += 4;
        if (n > size - pos) {
            fprintf(stderr, "%s: truncated record after %" PRIu64 " inputs\n", argv[1], count);
            free(pack);
            return 1;
        }
        fuzz_one(pack + pos, n);
        pos += n;
        count++;
    }
    printf("fuzzcase: %" PRIu64 " inputs\n", count);
    free(pack);
    return 0;
}

#endif
