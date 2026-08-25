// Differential driver for the Zig instantiation of the revera
// engine. It reads protocol commands on stdin, one per line, and
// prints one output line per command. The protocol is defined by
// go1/revera/driver_host.go, the Go reference implementation.

const std = @import("std");
const Io = std.Io;
const engine = @import("engine.zig");
const vg = @import("vg.zig");

const data = @embedFile("data.bin");

var inbuf: [1 << 20]u8 = undefined;
var outbuf: [1 << 16]u8 = undefined;
var rowsbuf: [1 << 22]u8 = undefined;

var base: engine.Locale = .{};
var cur: engine.Locale = .{};
var re: engine.Regexp = .{};
var valid = false;

fn decode(tok: []const u8) vg.Str {
    if (tok.len == 1 and tok[0] == '-') {
        return .{};
    }
    const s = vg.make(u8, @intCast(tok.len / 2));
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

fn handle(w: *Io.Writer, line: []const u8) !void {
    var it = std.mem.tokenizeScalar(u8, line, ' ');
    const cmd = next(&it);
    switch (cmd[0]) {
        'P' => {
            cur = engine.LocalePOSIX();
            try w.writeAll("P 1\n");
        },
        'L' => {
            vg.usePersistentArena();
            const name = decode(next(&it));
            const coll = decode(next(&it));
            const res = engine.LocaleSelect(&base, name, coll);
            vg.useScratchArena();
            if (res[1]) {
                cur = res[0];
            }
            try w.print("L {d}\n", .{@intFromBool(res[1])});
        },
        'C' => {
            const flags = parseU32(next(&it));
            const patTok = next(&it);
            valid = false;
            re = .{};
            vg.resetPatternArena();
            vg.usePatternArena();
            const pat = decode(patTok);
            const res = engine.Compile(pat, cur, flags);
            vg.useScratchArena();
            if (res[1].Code != 0) {
                try w.print("C {d} {d} 0\n", .{ res[1].Code, res[1].Pos });
                return;
            }
            re = res[0];
            valid = true;
            try w.print("C 0 0 {d}\n", .{engine.NumSub(&re)});
        },
        'X' => {
            if (!valid) {
                try w.writeAll("X ERR\n");
                return;
            }
            vg.resetScratchArena();
            const eflags = parseU32(next(&it));
            const subject = decode(next(&it));
            const pmatch = vg.make(engine.Match, engine.NumSub(&re) + 1);
            const res = engine.Exec(&re, subject, pmatch, eflags);
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
            if (!valid) {
                try w.writeAll("R ERR\n");
                return;
            }
            vg.resetScratchArena();
            const limit = parseI64(next(&it));
            const eflags = parseU32(next(&it));
            const repl = decode(next(&it));
            const subject = decode(next(&it));
            const res = engine.ReplaceAll(&re, subject, repl, limit, eflags);
            if (res[1].Code != 0) {
                try w.print("R {d} {d} -\n", .{ res[1].Code, res[1].Pos });
                return;
            }
            try w.writeAll("R 0 0 ");
            try writeHex(w, res[0]);
            try w.writeAll("\n");
        },
        'I' => {
            if (!valid) {
                try w.writeAll("I ERR\n");
                return;
            }
            vg.resetScratchArena();
            const limit = parseI64(next(&it));
            const eflags = parseU32(next(&it));
            const subject = decode(next(&it));
            const ires = engine.MatchIterInit(&re, limit);
            if (ires[1].Code != 0) {
                try w.print("I {d} 0\n", .{ires[1].Code});
                return;
            }
            var iter = ires[0];
            const pmatch = vg.make(engine.Match, engine.NumSub(&re) + 1);
            var rw = Io.Writer.fixed(&rowsbuf);
            var count: i64 = 0;
            while (true) {
                const res = engine.MatchIterNext(&re, &iter, subject, eflags, pmatch);
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
            if (!valid) {
                try w.writeAll("T ERR\n");
                return;
            }
            vg.resetScratchArena();
            const maxInput = parseI64(next(&it));
            var c = engine.ContractFor(&re, maxInput);
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
            var h: u64 = 0xcbf29ce484222325;
            var r: i32 = lo;
            while (r < hi) : (r +%= 1) {
                h ^= @as(u32, @bitCast(engine.localeToUpper(&cur, r)));
                h *%= 0x100000001b3;
                h ^= @as(u32, @bitCast(engine.localeToLower(&cur, r)));
                h *%= 0x100000001b3;
            }
            try w.print("O {d}\n", .{h});
        },
        else => @panic("unknown driver command"),
    }
}

pub fn main(init: std.process.Init) !void {
    const io = init.io;
    var out_w: Io.File.Writer = .init(.stdout(), io, &outbuf);
    const w = &out_w.interface;
    var in_r: Io.File.Reader = .init(.stdin(), io, &inbuf);
    const r = &in_r.interface;

    vg.usePersistentArena();
    const ld = engine.LocaleLoad(vg.str(data));
    if (!ld[1]) {
        @panic("embedded locale data failed to load");
    }
    base = ld[0];
    cur = engine.LocalePOSIX();
    vg.useScratchArena();

    while (try r.takeDelimiter('\n')) |line| {
        if (line.len == 0) {
            continue;
        }
        try handle(w, line);
        try w.flush();
    }
    try w.flush();
}
