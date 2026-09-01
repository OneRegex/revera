// Minimal runtime for the generated Vego engine.
// It supplies what the Go runtime supplied implicitly: an allocator, immutable string views, and the arithmetic helpers.
// The slice machinery is not here.
// C has no templates, so json2c monomorphizes the slice types and their helpers into the generated header.
//
// Memory is explicit.
// Every generated function that allocates takes an arena pointer as its first parameter.
// The runtime therefore holds no state at all.
// The host owns the arenas and decides which one backs each engine call.
// Two threads with separate arenas never share anything mutable.

#ifndef VG_H
#define VG_H

#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// vg_arena is a growing list of malloc blocks that free together.
// Its zero value is a valid empty arena.
// The counters are cumulative over the life of the arena and survive vg_arena_reset().
// They record the sizes the engine asks for, not what malloc rounds them up to.
typedef struct {
    void **blocks;
    size_t block_count;
    size_t block_cap;
    uint64_t alloc_count;
    uint64_t alloc_bytes;
} vg_arena;

static inline void *vg_arena_alloc(vg_arena *mem, size_t n) {
    mem->alloc_count++;
    mem->alloc_bytes += n;
    void *p = malloc(n ? n : 1);
    if (p == NULL) {
        abort();
    }
    if (mem->block_count == mem->block_cap) {
        size_t cap = mem->block_cap ? mem->block_cap * 2 : 64;
        void **blocks = (void **)realloc(mem->blocks, cap * sizeof(void *));
        if (blocks == NULL) {
            abort();
        }
        mem->blocks = blocks;
        mem->block_cap = cap;
    }
    mem->blocks[mem->block_count++] = p;
    return p;
}

// vg_arena_reset frees every block and keeps the block list for reuse.
static inline void vg_arena_reset(vg_arena *mem) {
    for (size_t i = 0; i < mem->block_count; i++) {
        free(mem->blocks[i]);
    }
    mem->block_count = 0;
}

// vg_arena_free releases everything, including the block list itself.
static inline void vg_arena_free(vg_arena *mem) {
    vg_arena_reset(mem);
    free(mem->blocks);
    mem->blocks = NULL;
    mem->block_cap = 0;
}

// vg_str is an immutable byte view, the translation of a Go string.
// Its zero value is the empty string.
typedef struct {
    const char *p;
    int64_t len;
} vg_str;

// vg_lit wraps a string literal.
// The sizeof form keeps embedded zero bytes, which strlen would lose.
#define vg_lit(s) ((vg_str){"" s, (int64_t)sizeof(s) - 1})

static inline uint8_t vg_str_at(vg_str s, int64_t i) {
    assert(i >= 0 && i < s.len);
    return (uint8_t)s.p[i];
}

static inline vg_str vg_str_sub(vg_str s, int64_t lo, int64_t hi) {
    assert(0 <= lo && lo <= hi && hi <= s.len);
    if (s.p == NULL) {
        return (vg_str){0};
    }
    return (vg_str){s.p + lo, hi - lo};
}

static inline vg_str vg_str_tail(vg_str s, int64_t lo) {
    return vg_str_sub(s, lo, s.len);
}

static inline vg_str vg_str_head(vg_str s, int64_t hi) {
    return vg_str_sub(s, 0, hi);
}

static inline bool vg_streq(vg_str a, vg_str b) {
    if (a.len != b.len) {
        return false;
    }
    return a.len == 0 || memcmp(a.p, b.p, (size_t)a.len) == 0;
}

static inline int32_t vg_strcmp3(vg_str a, vg_str b) {
    size_t n = (size_t)(a.len < b.len ? a.len : b.len);
    int c = n ? memcmp(a.p, b.p, n) : 0;
    if (c != 0) {
        return c < 0 ? -1 : 1;
    }
    if (a.len != b.len) {
        return a.len < b.len ? -1 : 1;
    }
    return 0;
}

// vg_str_dup copies a string into the arena, so the result outlives the buffer it came from.
static inline vg_str vg_str_dup(vg_arena *mem, vg_str s) {
    char *p = (char *)vg_arena_alloc(mem, (size_t)(s.len < 1 ? 1 : s.len));
    if (s.len > 0) {
        memcpy(p, s.p, (size_t)s.len);
    }
    return (vg_str){p, s.len};
}

// vg_sdiv and vg_srem are Go's truncating division and remainder.
// Go defines MinInt / -1 as MinInt, which wraps, and MinInt % -1 as 0.
// C leaves that pair undefined even with -fwrapv.

static inline int64_t vg_sdiv_i64(int64_t a, int64_t b) {
    assert(b != 0);
    if (b == -1) {
        return (int64_t)(0ULL - (uint64_t)a);
    }
    return a / b;
}

static inline int64_t vg_srem_i64(int64_t a, int64_t b) {
    assert(b != 0);
    if (b == -1) {
        return 0;
    }
    return a % b;
}

static inline int32_t vg_sdiv_i32(int32_t a, int32_t b) {
    assert(b != 0);
    if (b == -1) {
        return (int32_t)(0U - (uint32_t)a);
    }
    return a / b;
}

static inline int32_t vg_srem_i32(int32_t a, int32_t b) {
    assert(b != 0);
    if (b == -1) {
        return 0;
    }
    return a % b;
}

// The min and max helpers are functions, not macros, so each argument evaluates once.

static inline int64_t vg_min_i64(int64_t a, int64_t b) { return a < b ? a : b; }
static inline int64_t vg_max_i64(int64_t a, int64_t b) { return a > b ? a : b; }
static inline int32_t vg_min_i32(int32_t a, int32_t b) { return a < b ? a : b; }
static inline int32_t vg_max_i32(int32_t a, int32_t b) { return a > b ? a : b; }
static inline uint8_t vg_min_u8(uint8_t a, uint8_t b) { return a < b ? a : b; }
static inline uint8_t vg_max_u8(uint8_t a, uint8_t b) { return a > b ? a : b; }
static inline uint16_t vg_min_u16(uint16_t a, uint16_t b) { return a < b ? a : b; }
static inline uint16_t vg_max_u16(uint16_t a, uint16_t b) { return a > b ? a : b; }
static inline uint32_t vg_min_u32(uint32_t a, uint32_t b) { return a < b ? a : b; }
static inline uint32_t vg_max_u32(uint32_t a, uint32_t b) { return a > b ? a : b; }
static inline uint64_t vg_min_u64(uint64_t a, uint64_t b) { return a < b ? a : b; }
static inline uint64_t vg_max_u64(uint64_t a, uint64_t b) { return a > b ? a : b; }

#endif
