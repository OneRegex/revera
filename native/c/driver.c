// Differential driver for the C instantiation of the revera engine.
// It reads protocol commands on stdin, one per line, and prints one output line per command.
// dev/internal/protocol/driver.go, the Go reference implementation, defines the protocol.
//
// The host owns three arenas.
// Locale data lives in the persistent arena.
// A compiled pattern lives in the pattern arena until the next compile.
// Everything one operation allocates comes from the scratch arena, which resets before each operation.
// Each engine call receives the arena that must back its allocations.

#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "host.h"

static void print_hex(vg_str s) {
    if (s.len == 0) {
        fputs("-", stdout);
        return;
    }
    for (int64_t i = 0; i < s.len; i++) {
        printf("%02x", (unsigned)vg_str_at(s, i));
    }
}

// strbuf is a growable byte buffer for the row list of the I command.
// It replaces the std::string of the C++ host.
typedef struct {
    char *p;
    size_t len;
    size_t cap;
} strbuf;

static void sb_append(strbuf *sb, const char *s) {
    size_t n = strlen(s);
    if (sb->len + n + 1 > sb->cap) {
        size_t cap = sb->cap ? sb->cap : 256;
        while (sb->len + n + 1 > cap) {
            cap *= 2;
        }
        char *p = (char *)realloc(sb->p, cap);
        if (p == NULL) {
            abort();
        }
        sb->p = p;
        sb->cap = cap;
    }
    memcpy(sb->p + sb->len, s, n + 1);
    sb->len += n;
}

int main(void) {
    vg_arena persistent = {0};
    vg_arena pattern = {0};
    vg_arena scratch = {0};

    ENG(Locale) base_loc = base_locale();
    ENG(Locale) cur_loc = ENG(LocalePOSIX)();
    ENG(Regexp) cur_re = {0};
    bool re_valid = false;

    size_t line_cap = 0;
    char *line = NULL;
    while (read_line(&line, &line_cap) >= 0) {
        char *cursor = line;
        char *cmd = tok_next(&cursor);
        if (cmd == NULL) {
            continue;
        }
        switch (cmd[0]) {
        case 'P': {
            cur_loc = ENG(LocalePOSIX)();
            puts("P 1");
            break;
        }
        case 'L': {
            vg_str name = decode(&persistent, tok_next(&cursor));
            vg_str coll = decode(&persistent, tok_next(&cursor));
            ENG_TUPLE(Tup_t4c6f63616c65x_bool) res =
                ENG(LocaleSelect)(&persistent, &base_loc, name, coll);
            if (res.r1) {
                cur_loc = res.r0;
            }
            printf("L %d\n", res.r1 ? 1 : 0);
            break;
        }
        case 'C': {
            uint32_t flags = (uint32_t)strtoul(tok_next(&cursor), NULL, 10);
            const char *patTok = tok_next(&cursor);
            re_valid = false;
            cur_re = (ENG(Regexp)){0};
            vg_arena_reset(&pattern);
            vg_arena_reset(&scratch);
            vg_str pat = decode(&pattern, patTok);
            ENG_TUPLE(Tup_t526567657870x_t4572726f72x) res =
                ENG(Compile)(&pattern, pat, cur_loc, flags);
            if (res.r1.vego_f_Code != 0) {
                printf("C %" PRId32 " %" PRId64 " 0\n", res.r1.vego_f_Code, res.r1.vego_f_Pos);
                break;
            }
            cur_re = res.r0;
            re_valid = true;
            printf("C 0 0 %" PRId64 "\n", ENG(NumSub)(&cur_re));
            break;
        }
        case 'X': {
            if (!re_valid) {
                puts("X ERR");
                break;
            }
            vg_arena_reset(&scratch);
            uint32_t eflags = (uint32_t)strtoul(tok_next(&cursor), NULL, 10);
            vg_str subject = decode(&scratch, tok_next(&cursor));
            ENG_TYPE(st4d61746368x) pmatch =
                ENG_TYPE(st4d61746368x_make)(&scratch, ENG(NumSub)(&cur_re) + 1);
            ENG_TUPLE(Tup_bool_t4572726f72x) res =
                ENG(Exec)(&scratch, &cur_re, subject, pmatch, eflags);
            if (res.r1.vego_f_Code != 0) {
                printf("X %" PRId32 " 0\n", res.r1.vego_f_Code);
                break;
            }
            if (!res.r0) {
                puts("X 0 0");
                break;
            }
            fputs("X 0 1", stdout);
            for (int64_t i = 0; i < pmatch.len; i++) {
                printf(" %" PRId64 ",%" PRId64, pmatch.p[i].vego_f_So, pmatch.p[i].vego_f_Eo);
            }
            putchar('\n');
            break;
        }
        case 'R': {
            if (!re_valid) {
                puts("R ERR");
                break;
            }
            vg_arena_reset(&scratch);
            int64_t limit = strtoll(tok_next(&cursor), NULL, 10);
            uint32_t eflags = (uint32_t)strtoul(tok_next(&cursor), NULL, 10);
            vg_str repl = decode(&scratch, tok_next(&cursor));
            vg_str subject = decode(&scratch, tok_next(&cursor));
            ENG_TUPLE(Tup_Str_t4572726f72x) res =
                ENG(ReplaceAll)(&scratch, &cur_re, subject, repl, limit, eflags);
            if (res.r1.vego_f_Code != 0) {
                printf("R %" PRId32 " %lld -\n", res.r1.vego_f_Code, (long long)res.r1.vego_f_Pos);
                break;
            }
            fputs("R 0 0 ", stdout);
            print_hex(res.r0);
            putchar('\n');
            break;
        }
        case 'I': {
            if (!re_valid) {
                puts("I ERR");
                break;
            }
            vg_arena_reset(&scratch);
            int64_t limit = strtoll(tok_next(&cursor), NULL, 10);
            uint32_t eflags = (uint32_t)strtoul(tok_next(&cursor), NULL, 10);
            vg_str subject = decode(&scratch, tok_next(&cursor));
            ENG_TUPLE(Tup_t4d6174636849746572x_t4572726f72x) ires =
                ENG(MatchIterInit)(&cur_re, limit);
            if (ires.r1.vego_f_Code != 0) {
                printf("I %" PRId32 " 0\n", ires.r1.vego_f_Code);
                break;
            }
            ENG(MatchIter) iter = ires.r0;
            ENG_TYPE(st4d61746368x) pmatch =
                ENG_TYPE(st4d61746368x_make)(&scratch, ENG(NumSub)(&cur_re) + 1);
            strbuf rows = {0};
            int64_t count = 0;
            bool failed = false;
            for (;;) {
                ENG_TUPLE(Tup_bool_t4572726f72x) res =
                    ENG(MatchIterNext)(&scratch, &cur_re, &iter, subject, eflags, pmatch);
                if (res.r1.vego_f_Code != 0) {
                    printf("I %" PRId32 " 0\n", res.r1.vego_f_Code);
                    failed = true;
                    break;
                }
                if (!res.r0) {
                    break;
                }
                if (count > 0) {
                    sb_append(&rows, "|");
                }
                char buf[64];
                for (int64_t i = 0; i < pmatch.len; i++) {
                    snprintf(buf, sizeof(buf), "%s%" PRId64 ",%" PRId64,
                             i > 0 ? "," : "", pmatch.p[i].vego_f_So, pmatch.p[i].vego_f_Eo);
                    sb_append(&rows, buf);
                }
                count++;
            }
            if (failed) {
                free(rows.p);
                break;
            }
            printf("I 0 %" PRId64, count);
            if (count > 0) {
                printf(" %s", rows.p);
            }
            putchar('\n');
            free(rows.p);
            break;
        }
        case 'T': {
            if (!re_valid) {
                puts("T ERR");
                break;
            }
            vg_arena_reset(&scratch);
            int64_t maxInput = strtoll(tok_next(&cursor), NULL, 10);
            ENG(Contract) c = ENG(ContractFor)(&cur_re, maxInput);
            printf("T %d %" PRId64 " %" PRId64 " %" PRId64 "\n",
                   c.vego_f_HasSolver ? 1 : 0, ENG(ContractHeapBytes)(&c),
                   ENG(ContractStackBytes)(&c), ENG(ContractSteps)(&c));
            break;
        }
        case 'O': {
            int64_t lo = strtoll(tok_next(&cursor), NULL, 10);
            int64_t hi = strtoll(tok_next(&cursor), NULL, 10);
            uint64_t h = 0xcbf29ce484222325ULL;
            int32_t hi32 = (int32_t)hi;
            for (int32_t r = (int32_t)lo; r < hi32; r++) {
                h ^= (uint64_t)(uint32_t)ENG(localeToUpper)(&cur_loc, r);
                h *= 0x100000001b3ULL;
                h ^= (uint64_t)(uint32_t)ENG(localeToLower)(&cur_loc, r);
                h *= 0x100000001b3ULL;
            }
            printf("O %" PRIu64 "\n", h);
            break;
        }
        default:
            fputs("unknown driver command\n", stderr);
            return 1;
        }
        fflush(stdout);
    }
    free(line);
    vg_arena_free(&scratch);
    vg_arena_free(&pattern);
    vg_arena_free(&persistent);
    return 0;
}
