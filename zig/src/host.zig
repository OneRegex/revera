// Helpers shared by the hand-written hosts: the driver, the bench binary, the fuzz entry point and the seed pack runner.
// The locale blob is embedded here once, and every host loads it through loadBase.

const std = @import("std");
const engine = @import("engine.zig");
const vg = @import("vg.zig");

pub const data = @embedFile("data.bin");

// loadBase validates the embedded locale blob.
// That costs a pass over 1.8 MB, so a process calls it once and keeps the result.
pub fn loadBase() engine.Locale {
    const ld = engine.LocaleLoad(vg.str(data));
    if (!ld[1]) {
        @panic("embedded locale data failed to load");
    }
    return ld[0];
}

// decode turns one hex token of the line protocol into a string.
// A lone dash stands for the empty string.
pub fn decode(gpa: vg.Allocator, tok: []const u8) vg.Allocator.Error!vg.Str {
    if (tok.len == 1 and tok[0] == '-') {
        return .{};
    }
    const s = try vg.make(gpa, u8, @intCast(tok.len / 2));
    _ = std.fmt.hexToBytes(s.items(), tok) catch @panic("bad hex");
    return vg.str(s.items());
}

pub fn parseI64(tok: []const u8) i64 {
    return std.fmt.parseInt(i64, tok, 10) catch @panic("bad integer");
}

pub fn parseU32(tok: []const u8) u32 {
    return std.fmt.parseInt(u32, tok, 10) catch @panic("bad integer");
}

pub fn next(it: *std.mem.TokenIterator(u8, .scalar)) []const u8 {
    return it.next() orelse @panic("missing token");
}
