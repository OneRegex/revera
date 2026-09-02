// Benchmark host for the C11 instantiation of the revera engine.
// It reads bench protocol commands on stdin, one per line, and prints one answer line per command.
// dev/internal/protocol/bench.go, the Go reference implementation, defines the protocol.
//
// The host owns four arenas.
// Locale data lives in the persistent arena.
// The decoded strings of one B command live in the input arena, which resets at the start of each B command.
// A compile iteration resets the pattern arena, and a match or replace iteration resets the scratch arena.
// The untimed pass reads the allocation counters of those two arenas.
// The timed passes read a monotonic clock around a whole pass.

#define _POSIX_C_SOURCE 199309L

#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "host.h"

typedef enum { KIND_COMPILE, KIND_MATCH, KIND_REPLACE } bench_kind;

// bench_case holds the inputs of one B command.
// The compile kind reads pat, loc and cflags, and the other kinds read re, subject and repl.
typedef struct {
    bench_kind kind;
    vg_str pat;
    revera_eng_Locale loc;
    uint32_t cflags;
    vg_str subject;
    vg_str repl;
    revera_eng_Regexp re;
} bench_case;

// A B command has a fixed shape, so a missing token is a protocol error.
static char *tok_need(char **cursor) {
    char *t = tok_next(cursor);
    if (t == NULL) {
        fputs("malformed bench command\n", stderr);
        exit(1);
    }
    return t;
}

// now_ns reads the monotonic clock and converts the timespec to nanoseconds.
static int64_t now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (int64_t)ts.tv_sec * 1000000000 + (int64_t)ts.tv_nsec;
}

// run_pass performs iters operations of the kind the case names.
// Each kind is a plain loop, so nothing but the engine call sits inside the timed region.
static void run_pass(bench_case *c, int64_t iters, vg_arena *pattern, vg_arena *scratch) {
    switch (c->kind) {
    case KIND_COMPILE:
        for (int64_t i = 0; i < iters; i++) {
            vg_arena_reset(pattern);
            revera_eng_Compile(pattern, c->pat, c->loc, c->cflags);
        }
        break;
    case KIND_MATCH: {
        int64_t nmatch = revera_eng_NumSub(&c->re) + 1;
        for (int64_t i = 0; i < iters; i++) {
            vg_arena_reset(scratch);
            revera_eng_slice_Match pmatch = revera_eng_slice_Match_make(scratch, nmatch);
            revera_eng_Exec(scratch, &c->re, c->subject, pmatch, 0);
        }
        break;
    }
    case KIND_REPLACE:
        for (int64_t i = 0; i < iters; i++) {
            vg_arena_reset(scratch);
            revera_eng_ReplaceAll(scratch, &c->re, c->subject, c->repl, -1, 0);
        }
        break;
    }
}

int main(void) {
    vg_arena persistent = {0};
    vg_arena input = {0};
    vg_arena pattern = {0};
    vg_arena scratch = {0};

    revera_eng_Locale base_loc = base_locale();
    revera_eng_Locale cur_loc = revera_eng_LocalePOSIX();

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
            cur_loc = revera_eng_LocalePOSIX();
            puts("P 1");
            break;
        }
        case 'L': {
            vg_str name = decode(&persistent, tok_need(&cursor));
            vg_str coll = decode(&persistent, tok_need(&cursor));
            revera_eng_Tup_t4c6f63616c65x_bool res = revera_eng_LocaleSelect(&persistent, &base_loc, name, coll);
            if (res.r1) {
                cur_loc = res.r0;
            }
            printf("L %d\n", res.r1 ? 1 : 0);
            break;
        }
        case 'B': {
            char *name = tok_need(&cursor);
            char *kind_tok = tok_need(&cursor);
            int64_t iters = strtoll(tok_need(&cursor), NULL, 10);
            int64_t reps = strtoll(tok_need(&cursor), NULL, 10);
            bench_case c;
            c.cflags = (uint32_t)strtoul(tok_need(&cursor), NULL, 10);
            vg_arena_reset(&input);
            c.pat = decode(&input, tok_need(&cursor));
            c.subject = decode(&input, tok_need(&cursor));
            c.repl = decode(&input, tok_need(&cursor));
            c.loc = cur_loc;
            if (strcmp(kind_tok, "compile") == 0) {
                c.kind = KIND_COMPILE;
            } else if (strcmp(kind_tok, "match") == 0) {
                c.kind = KIND_MATCH;
            } else if (strcmp(kind_tok, "replace") == 0) {
                c.kind = KIND_REPLACE;
            } else {
                fprintf(stderr, "unknown bench kind %s\n", kind_tok);
                return 1;
            }
            if (iters <= 0) {
                fputs("bench iters must be positive\n", stderr);
                return 1;
            }
            vg_arena_reset(&pattern);
            vg_arena_reset(&scratch);
            revera_eng_Tup_t526567657870x_t4572726f72x res = revera_eng_Compile(&pattern, c.pat, c.loc, c.cflags);
            if (res.r1.Code != 0) {
                printf("B %s %" PRId32 " 0 0\n", name, res.r1.Code);
                break;
            }
            c.re = res.r0;

            uint64_t bytes0 = pattern.alloc_bytes + scratch.alloc_bytes;
            uint64_t allocs0 = pattern.alloc_count + scratch.alloc_count;
            run_pass(&c, iters, &pattern, &scratch);
            uint64_t bytes = (pattern.alloc_bytes + scratch.alloc_bytes - bytes0) / (uint64_t)iters;
            uint64_t allocs = (pattern.alloc_count + scratch.alloc_count - allocs0) / (uint64_t)iters;
            printf("B %s 0 %" PRIu64 " %" PRIu64, name, bytes, allocs);

            for (int64_t r = 0; r < reps; r++) {
                int64_t start = now_ns();
                run_pass(&c, iters, &pattern, &scratch);
                int64_t ns = now_ns() - start;
                printf(" %" PRId64, ns);
            }
            putchar('\n');
            break;
        }
        default:
            fputs("unknown bench command\n", stderr);
            return 1;
        }
        fflush(stdout);
    }
    free(line);
    vg_arena_free(&scratch);
    vg_arena_free(&pattern);
    vg_arena_free(&input);
    vg_arena_free(&persistent);
    return 0;
}
