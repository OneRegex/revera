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

const data = @embedFile("data.bin");

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

fn decode(gpa: vg.Allocator, tok: []const u8) vg.Str {
    if (tok.len == 1 and tok[0] == '-') {
        return .{};
    }
    const s = vg.make(gpa, u8, @intCast(tok.len / 2));
    _ = std.fmt.hexToBytes(s.items(), tok) catch @panic("bad hex");
    return vg.str(s.items());
}

fn parseI64(tok: []const u8) i64 {
    return std.fmt.parseInt(i64, tok, 10) catch @panic("bad integer");
}

fn parseU32(tok: []const u8) u32 {
    return std.fmt.parseInt(u32, tok, 10) catch @panic("bad integer");
}

fn writeHex(w: *Io.Writer, s: vg.Str) !void {
    if (s.len == 0) {
        try w.writeAll("-");
        return;
    }
    try w.print("{x}", .{s.bytes()});
}

fn next(it: *std.mem.TokenIterator(u8, .scalar)) []const u8 {
    return it.next() orelse @panic("missing token");
}

fn handle(h: *Host, w: *Io.Writer, line: []const u8) !void {
    var it = std.mem.tokenizeScalar(u8, line, ' ');
    const cmd = next(&it);
    switch (cmd[0]) {
        'P' => {
            h.cur = engine.LocalePOSIX();
            try w.writeAll("P 1\n");
        },
        'L' => {
            const name = decode(h.persistent, next(&it));
            const coll = decode(h.persistent, next(&it));
            const res = engine.LocaleSelect(h.persistent, &h.base, name, coll);
            if (res[1]) {
                h.cur = res[0];
            }
            try w.print("L {d}\n", .{@intFromBool(res[1])});
        },
        'C' => {
            const flags = parseU32(next(&it));
            const patTok = next(&it);
            h.valid = false;
            h.re = .{};
            _ = h.pattern.reset(.retain_capacity);
            _ = h.scratch.reset(.retain_capacity);
            const gpa = h.pattern.allocator();
            const pat = decode(gpa, patTok);
            const res = engine.Compile(gpa, pat, h.cur, flags);
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
            const eflags = parseU32(next(&it));
            const subject = decode(gpa, next(&it));
            const pmatch = vg.make(gpa, engine.Match, engine.NumSub(&h.re) + 1);
            const res = engine.Exec(gpa, &h.re, subject, pmatch, eflags);
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
            const limit = parseI64(next(&it));
            const eflags = parseU32(next(&it));
            const repl = decode(gpa, next(&it));
            const subject = decode(gpa, next(&it));
            const res = engine.ReplaceAll(gpa, &h.re, subject, repl, limit, eflags);
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
            const limit = parseI64(next(&it));
            const eflags = parseU32(next(&it));
            const subject = decode(gpa, next(&it));
            const ires = engine.MatchIterInit(&h.re, limit);
            if (ires[1].Code != 0) {
                try w.print("I {d} 0\n", .{ires[1].Code});
                return;
            }
            var iter = ires[0];
            const pmatch = vg.make(gpa, engine.Match, engine.NumSub(&h.re) + 1);
            const rows = try gpa.alloc(u8, 1 << 22);
            var rw = Io.Writer.fixed(rows);
            var count: i64 = 0;
            while (true) {
                const res = engine.MatchIterNext(gpa, &h.re, &iter, subject, eflags, pmatch);
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
                try w.writeAll(rw.buffered());
            }
            try w.writeAll("\n");
        },
        'T' => {
            if (!h.valid) {
                try w.writeAll("T ERR\n");
                return;
            }
            _ = h.scratch.reset(.retain_capacity);
            const maxInput = parseI64(next(&it));
            var c = engine.ContractFor(&h.re, maxInput);
            try w.print("T {d} {d} {d} {d}\n", .{
                @intFromBool(c.HasSolver),
                engine.ContractHeapBytes(&c),
                engine.ContractStackBytes(&c),
                engine.ContractSteps(&c),
            });
        },
        'O' => {
            const lo = vg.cv(i32, parseI64(next(&it)));
            const hi = vg.cv(i32, parseI64(next(&it)));
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

    const ld = engine.LocaleLoad(vg.str(data));
    if (!ld[1]) {
        @panic("embedded locale data failed to load");
    }
    h.base = ld[0];
    h.cur = engine.LocalePOSIX();

    while (try r.takeDelimiter('\n')) |line| {
        if (line.len == 0) {
            continue;
        }
        try handle(&h, w, line);
        try w.flush();
    }
    try w.flush();
}
