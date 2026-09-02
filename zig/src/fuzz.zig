// Fuzz entry point for the Zig instantiation of the revera engine.
// Every target shares the input layout, and dev/internal/protocol/fuzz.go is the reference.
//
//     byte 0        compile flags, masked with 0x0f
//     byte 1        bits 0..1 are the Exec flags, bit 4 selects locale "cs", else bit 5 selects locale "tr"
//     byte 2        n, the pattern length
//     bytes 3..     the pattern, n bytes or fewer if the input ends early
//     next byte     m, the replacement length
//     next m bytes  the replacement, fewer if the input ends early
//     rest          the subject
//
// The property is crash freedom.
// Every result is ignored.

const std = @import("std");
const engine = @import("engine.zig");
const vg = @import("vg.zig");
const host = @import("host.zig");

// fuzzOne runs the whole engine surface on one input.
// All memory comes from the caller's arena, and the arena resets once the input is done, so a long session stays flat.
pub fn fuzzOne(arena: *std.heap.ArenaAllocator, base: *engine.Locale, input: []const u8) !void {
    if (input.len < 3) {
        return;
    }
    defer _ = arena.reset(.retain_capacity);
    const mem = arena.allocator();

    const cflags: u32 = input[0] & 0x0f;
    const eflags: u32 = input[1] & 0x03;
    var loc = engine.LocalePOSIX();
    if (input[1] & 0x10 != 0) {
        const res = try engine.LocaleSelect(mem, base, vg.lit("cs"), .{});
        if (!res[1]) {
            return;
        }
        loc = res[0];
    } else if (input[1] & 0x20 != 0) {
        const res = try engine.LocaleSelect(mem, base, vg.lit("tr"), .{});
        if (!res[1]) {
            return;
        }
        loc = res[0];
    }

    var rest = input[3..];
    const pattern = rest[0..@min(@as(usize, input[2]), rest.len)];
    rest = rest[pattern.len..];
    var replacement: []const u8 = &.{};
    if (rest.len > 0) {
        const m: usize = rest[0];
        rest = rest[1..];
        replacement = rest[0..@min(m, rest.len)];
        rest = rest[replacement.len..];
    }
    const subject = vg.str(rest);

    const cres = try engine.Compile(mem, vg.str(pattern), loc, cflags);
    if (cres[1].Code != 0) {
        return;
    }
    var re = cres[0];

    const pmatch = try vg.make(mem, engine.Match, engine.NumSub(&re) + 1);
    _ = try engine.Exec(mem, &re, subject, pmatch, eflags);
    _ = try engine.ReplaceAll(mem, &re, subject, vg.str(replacement), -1, eflags);

    const ires = engine.MatchIterInit(&re, 3);
    if (ires[1].Code == 0) {
        var iter = ires[0];
        while (true) {
            const res = try engine.MatchIterNext(mem, &re, &iter, subject, eflags, pmatch);
            if (res[1].Code != 0 or !res[0]) {
                break;
            }
        }
    }

    var c = engine.ContractFor(&re, subject.len);
    _ = engine.ContractHeapBytes(&c);
    _ = engine.ContractStackBytes(&c);
    _ = engine.ContractSteps(&c);
}

// Context is what the test hands to std.testing.fuzz.
// The arena sits on the page allocator, not on std.testing.allocator.
// In fuzz mode the runner creates a fresh testing allocator for every input and fails on anything still allocated afterwards.
// An arena that keeps its capacity across inputs would trip that check on the second input.
const Context = struct {
    arena: std.heap.ArenaAllocator,
    base: engine.Locale,
};

fn testOne(ctx: *Context, smith: *std.testing.Smith) !void {
    var buf: [1024]u8 = undefined;
    const n = smith.slice(&buf);
    try fuzzOne(&ctx.arena, &ctx.base, buf[0..n]);
}

// A corpus entry feeds Smith.slice, which reads a little-endian u32 length before the bytes.
// seed adds that prefix so the entry reaches fuzzOne as written.
inline fn seed(comptime bytes: []const u8) []const u8 {
    return comptime blk: {
        var out: [4 + bytes.len]u8 = undefined;
        std.mem.writeInt(u32, out[0..4], bytes.len, .little);
        @memcpy(out[4..], bytes);
        const frozen = out;
        break :blk &frozen;
    };
}

test "engine fuzz" {
    var ctx: Context = .{
        .arena = std.heap.ArenaAllocator.init(std.heap.page_allocator),
        .base = host.loadBase(),
    };
    defer ctx.arena.deinit();
    try std.testing.fuzz(&ctx, testOne, .{ .corpus = &.{
        seed("\x00\x00\x06(a|b)*\x01xababab"),
        seed("\x01\x10\x0c([[.ch.]]|c)\x03\\1-chcchxch"),
        seed("\x08\x22\x05(a*?)\x00\naaa"),
    } });
}
