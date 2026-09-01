// Differential driver for the Zig instantiation of the revera engine.
// It reads protocol commands on stdin, one per line, and prints one output line per command.
// go1/revera/driver_host.go, the Go reference implementation, defines the protocol.
//
// The host owns three arenas.
// Locale data lives in the persistent arena.
// A compiled pattern lives in the pattern arena until the next compile.
// Everything one operation allocates comes from the scratch arena, which resets before each operation.
// Each engine call receives the arena that must back its allocations.

const std = @import("std");
const Io = std.Io;
const engine = @import("engine.zig");
const vg = @import("vg.zig");
const host = @import("host.zig");

// The persistent arena never resets, so its plain allocator is enough.
// The pattern and scratch arenas stay whole, because they need reset().
const Host = struct {
    persistent: vg.Allocator,
    pattern: *std.heap.ArenaAllocator,
    scratch: *std.heap.ArenaAllocator,
    base: engine.Locale = .{},
    cur: engine.Locale = .{},
    re: engine.Regexp = .{},
    valid: bool = false,
};

fn writeHex(w: *Io.Writer, s: vg.Str) !void {
    if (s.len == 0) {
        try w.writeAll("-");
        return;
    }
    try w.print("{x}", .{s.bytes()});
}

fn handle(h: *Host, w: *Io.Writer, line: []const u8) !void {
    var it = std.mem.tokenizeScalar(u8, line, ' ');
    const cmd = host.next(&it);
    switch (cmd[0]) {
        'P' => {
            h.cur = engine.LocalePOSIX();
            try w.writeAll("P 1\n");
        },
        'L' => {
            const name = try host.decode(h.persistent, host.next(&it));
            const coll = try host.decode(h.persistent, host.next(&it));
            const res = try engine.LocaleSelect(h.persistent, &h.base, name, coll);
            if (res[1]) {
                h.cur = res[0];
            }
            try w.print("L {d}\n", .{@intFromBool(res[1])});
        },
        'C' => {
            const flags = host.parseU32(host.next(&it));
            const patTok = host.next(&it);
            h.valid = false;
            h.re = .{};
            _ = h.pattern.reset(.retain_capacity);
            _ = h.scratch.reset(.retain_capacity);
            const gpa = h.pattern.allocator();
            const pat = try host.decode(gpa, patTok);
            const res = try engine.Compile(gpa, pat, h.cur, flags);
            if (res[1].Code != 0) {
                try w.print("C {d} {d} 0\n", .{ res[1].Code, res[1].Pos });
                return;
            }
            h.re = res[0];
            h.valid = true;
            try w.print("C 0 0 {d}\n", .{engine.NumSub(&h.re)});
        },
        'X' => {
            if (!h.valid) {
                try w.writeAll("X ERR\n");
                return;
            }
            _ = h.scratch.reset(.retain_capacity);
            const gpa = h.scratch.allocator();
            const eflags = host.parseU32(host.next(&it));
            const subject = try host.decode(gpa, host.next(&it));
            const pmatch = try vg.make(gpa, engine.Match, engine.NumSub(&h.re) + 1);
            const res = try engine.Exec(gpa, &h.re, subject, pmatch, eflags);
            if (res[1].Code != 0) {
                try w.print("X {d} 0\n", .{res[1].Code});
                return;
            }
            if (!res[0]) {
                try w.writeAll("X 0 0\n");
                return;
            }
            try w.writeAll("X 0 1");
            for (pmatch.items()) |m| {
                try w.print(" {d},{d}", .{ m.So, m.Eo });
            }
            try w.writeAll("\n");
        },
        'R' => {
            if (!h.valid) {
                try w.writeAll("R ERR\n");
                return;
            }
            _ = h.scratch.reset(.retain_capacity);
            const gpa = h.scratch.allocator();
            const limit = host.parseI64(host.next(&it));
            const eflags = host.parseU32(host.next(&it));
            const repl = try host.decode(gpa, host.next(&it));
            const subject = try host.decode(gpa, host.next(&it));
            const res = try engine.ReplaceAll(gpa, &h.re, subject, repl, limit, eflags);
            if (res[1].Code != 0) {
                try w.print("R {d} {d} -\n", .{ res[1].Code, res[1].Pos });
                return;
            }
            try w.writeAll("R 0 0 ");
            try writeHex(w, res[0]);
            try w.writeAll("\n");
        },
        'I' => {
            if (!h.valid) {
                try w.writeAll("I ERR\n");
                return;
            }
            _ = h.scratch.reset(.retain_capacity);
            const gpa = h.scratch.allocator();
            const limit = host.parseI64(host.next(&it));
            const eflags = host.parseU32(host.next(&it));
            const subject = try host.decode(gpa, host.next(&it));
            const ires = engine.MatchIterInit(&h.re, limit);
            if (ires[1].Code != 0) {
                try w.print("I {d} 0\n", .{ires[1].Code});
                return;
            }
            var iter = ires[0];
            const pmatch = try vg.make(gpa, engine.Match, engine.NumSub(&h.re) + 1);
            // The rows grow with the match count.
            var rows: Io.Writer.Allocating = .init(gpa);
            const rw = &rows.writer;
            var count: i64 = 0;
            while (true) {
                const res = try engine.MatchIterNext(gpa, &h.re, &iter, subject, eflags, pmatch);
                if (res[1].Code != 0) {
                    try w.print("I {d} 0\n", .{res[1].Code});
                    return;
                }
                if (!res[0]) {
                    break;
                }
                if (count > 0) {
                    try rw.writeByte('|');
                }
                for (pmatch.items(), 0..) |m, i| {
                    if (i > 0) {
                        try rw.writeByte(',');
                    }
                    try rw.print("{d},{d}", .{ m.So, m.Eo });
                }
                count += 1;
            }
            try w.print("I 0 {d}", .{count});
            if (count > 0) {
                try w.writeAll(" ");
                try w.writeAll(rows.written());
            }
            try w.writeAll("\n");
        },
        'T' => {
            if (!h.valid) {
                try w.writeAll("T ERR\n");
                return;
            }
            _ = h.scratch.reset(.retain_capacity);
            const maxInput = host.parseI64(host.next(&it));
            var c = engine.ContractFor(&h.re, maxInput);
            try w.print("T {d} {d} {d} {d}\n", .{
                @intFromBool(c.HasSolver),
                engine.ContractHeapBytes(&c),
                engine.ContractStackBytes(&c),
                engine.ContractSteps(&c),
            });
        },
        'O' => {
            const lo = vg.cv(i32, host.parseI64(host.next(&it)));
            const hi = vg.cv(i32, host.parseI64(host.next(&it)));
            var hash: u64 = 0xcbf29ce484222325;
            var r: i32 = lo;
            while (r < hi) : (r +%= 1) {
                hash ^= @as(u32, @bitCast(engine.localeToUpper(&h.cur, r)));
                hash *%= 0x100000001b3;
                hash ^= @as(u32, @bitCast(engine.localeToLower(&h.cur, r)));
                hash *%= 0x100000001b3;
            }
            try w.print("O {d}\n", .{hash});
        },
        else => @panic("unknown driver command"),
    }
}

pub fn main(init: std.process.Init) !void {
    const io = init.io;
    const gpa = std.heap.page_allocator;

    var arena_persistent = std.heap.ArenaAllocator.init(gpa);
    defer arena_persistent.deinit();
    var arena_pattern = std.heap.ArenaAllocator.init(gpa);
    defer arena_pattern.deinit();
    var arena_scratch = std.heap.ArenaAllocator.init(gpa);
    defer arena_scratch.deinit();

    const inbuf = try gpa.alloc(u8, 1 << 20);
    defer gpa.free(inbuf);
    const outbuf = try gpa.alloc(u8, 1 << 16);
    defer gpa.free(outbuf);

    var out_w: Io.File.Writer = .init(.stdout(), io, outbuf);
    const w = &out_w.interface;
    var in_r: Io.File.Reader = .init(.stdin(), io, inbuf);
    const r = &in_r.interface;

    var h: Host = .{
        .persistent = arena_persistent.allocator(),
        .pattern = &arena_pattern,
        .scratch = &arena_scratch,
    };

    h.base = host.loadBase();
    h.cur = engine.LocalePOSIX();

    // A line has no fixed bound, because a subject may be up to 2^31-1 bytes.
    var line_w: Io.Writer.Allocating = .init(gpa);
    defer line_w.deinit();
    while (true) {
        line_w.clearRetainingCapacity();
        const ended = if (r.streamDelimiter(&line_w.writer, '\n')) |_| false else |err| switch (err) {
            error.EndOfStream => true,
            else => |e| return e,
        };
        if (!ended) {
            r.toss(1);
        }
        const line = line_w.written();
        if (line.len > 0) {
            try handle(&h, w, line);
            try w.flush();
        }
        if (ended) {
            break;
        }
    }
    try w.flush();
}
