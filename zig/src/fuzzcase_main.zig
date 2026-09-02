// Seed pack runner for the Zig fuzz entry point.
// Usage: fuzzcase <packfile>
//
// The pack is a sequence of records.
// Each record is a little-endian u32 length followed by that many bytes.
// Every record goes through fuzz.fuzzOne, and a crash or a failed assert is the signal.
// A missing or truncated pack is an error: a message on stderr and exit status 1.

const std = @import("std");
const Io = std.Io;
const fuzz = @import("fuzz.zig");
const host = @import("host.zig");

fn fail(comptime fmt: []const u8, args: anytype) noreturn {
    std.debug.print("fuzzcase: " ++ fmt ++ "\n", args);
    std.process.exit(1);
}

pub fn main(init: std.process.Init) !void {
    const io = init.io;
    const gpa = init.gpa;

    var args = init.minimal.args.iterate();
    _ = args.next();
    const path = args.next() orelse fail("usage: fuzzcase <packfile>", .{});

    const pack = Io.Dir.cwd().readFileAlloc(io, path, gpa, .unlimited) catch |err| {
        fail("cannot read {s}: {t}", .{ path, err });
    };
    defer gpa.free(pack);

    var arena = std.heap.ArenaAllocator.init(gpa);
    defer arena.deinit();
    var base = host.loadBase();
    var count: usize = 0;
    var off: usize = 0;
    while (off < pack.len) {
        if (pack.len - off < 4) {
            fail("truncated record header at offset {d}", .{off});
        }
        const n = std.mem.readInt(u32, pack[off..][0..4], .little);
        off += 4;
        if (pack.len - off < n) {
            fail("truncated record at offset {d}: {d} bytes announced, {d} left", .{ off, n, pack.len - off });
        }
        try fuzz.fuzzOne(&arena, &base, pack[off..][0..n]);
        off += n;
        count += 1;
    }

    var outbuf: [256]u8 = undefined;
    var out_w: Io.File.Writer = .init(.stdout(), io, &outbuf);
    const w = &out_w.interface;
    try w.print("fuzzcase: {d} inputs\n", .{count});
    try w.flush();
}
