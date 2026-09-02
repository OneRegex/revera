// Bench binary for the Zig instantiation of the revera engine.
// It speaks the line protocol that dev/internal/protocol/bench.go defines and dev/cmd/bench drives.
//
// The host owns four arenas.
// Locale data lives in the persistent arena.
// The decoded strings of one B command live in the input arena, which resets at the start of each B command.
// A compiled pattern lives in the pattern arena, and a compile iteration resets it.
// Everything one match or replace iteration allocates comes from the scratch arena, which resets before each iteration.
//
// The untimed pass runs the engine through a counting allocator that wraps the arena allocator.
// It records the requests the engine makes, not the pages the arena takes from the system.
// The timed passes use the plain arena allocator, so counting adds nothing to the figures.

const std = @import("std");
const Io = std.Io;
const engine = @import("engine.zig");
const vg = @import("vg.zig");
const host = @import("host.zig");

const Counting = struct {
    child: vg.Allocator,
    allocs: u64 = 0,
    bytes: u64 = 0,

    const vtable: vg.Allocator.VTable = .{
        .alloc = alloc,
        .resize = resize,
        .remap = remap,
        .free = free,
    };

    fn allocator(self: *Counting) vg.Allocator {
        return .{ .ptr = self, .vtable = &vtable };
    }

    fn alloc(ctx: *anyopaque, len: usize, alignment: std.mem.Alignment, ret_addr: usize) ?[*]u8 {
        const self: *Counting = @ptrCast(@alignCast(ctx));
        self.allocs += 1;
        self.bytes += len;
        return self.child.rawAlloc(len, alignment, ret_addr);
    }

    fn resize(ctx: *anyopaque, memory: []u8, alignment: std.mem.Alignment, new_len: usize, ret_addr: usize) bool {
        const self: *Counting = @ptrCast(@alignCast(ctx));
        return self.child.rawResize(memory, alignment, new_len, ret_addr);
    }

    fn remap(ctx: *anyopaque, memory: []u8, alignment: std.mem.Alignment, new_len: usize, ret_addr: usize) ?[*]u8 {
        const self: *Counting = @ptrCast(@alignCast(ctx));
        return self.child.rawRemap(memory, alignment, new_len, ret_addr);
    }

    fn free(ctx: *anyopaque, memory: []u8, alignment: std.mem.Alignment, ret_addr: usize) void {
        const self: *Counting = @ptrCast(@alignCast(ctx));
        self.child.rawFree(memory, alignment, ret_addr);
    }
};

const Host = struct {
    persistent: vg.Allocator,
    input: *std.heap.ArenaAllocator,
    pattern: *std.heap.ArenaAllocator,
    scratch: *std.heap.ArenaAllocator,
    base: engine.Locale = .{},
    cur: engine.Locale = .{},
};

const Kind = enum { compile, match, replace };

// Op holds what one iteration needs.
// The pattern, subject and replacement live in the input arena.
// The compiled expression lives in the pattern arena and only the match and replace kinds read it.
const Op = struct {
    kind: Kind,
    flags: u32,
    loc: engine.Locale,
    pattern: vg.Str,
    subject: vg.Str,
    repl: vg.Str,
    re: engine.Regexp = .{},
};

fn parseUsize(tok: []const u8) usize {
    return std.fmt.parseInt(usize, tok, 10) catch @panic("bad integer");
}

fn parseKind(tok: []const u8) Kind {
    return std.meta.stringToEnum(Kind, tok) orelse @panic("unknown bench kind");
}

// runPass runs iters iterations of the operation.
// arena is the arena an iteration resets, and mem is the allocator the engine receives.
// mem is either the plain allocator of that arena or a counting wrapper around it.
fn runPass(op: *Op, arena: *std.heap.ArenaAllocator, mem: vg.Allocator, iters: usize) !void {
    var i: usize = 0;
    switch (op.kind) {
        .compile => while (i < iters) : (i += 1) {
            _ = arena.reset(.retain_capacity);
            _ = try engine.Compile(mem, op.pattern, op.loc, op.flags);
        },
        .match => while (i < iters) : (i += 1) {
            _ = arena.reset(.retain_capacity);
            const pmatch = try vg.make(mem, engine.Match, engine.NumSub(&op.re) + 1);
            _ = try engine.Exec(mem, &op.re, op.subject, pmatch, 0);
        },
        .replace => while (i < iters) : (i += 1) {
            _ = arena.reset(.retain_capacity);
            _ = try engine.ReplaceAll(mem, &op.re, op.subject, op.repl, -1, 0);
        },
    }
}

fn bench(h: *Host, io: Io, w: *Io.Writer, it: *std.mem.TokenIterator(u8, .scalar)) !void {
    const name = host.next(it);
    const kind = parseKind(host.next(it));
    const iters = parseUsize(host.next(it));
    const reps = parseUsize(host.next(it));
    const flags = host.parseU32(host.next(it));

    _ = h.input.reset(.retain_capacity);
    const in = h.input.allocator();
    var op: Op = .{
        .kind = kind,
        .flags = flags,
        .loc = h.cur,
        .pattern = try host.decode(in, host.next(it)),
        .subject = try host.decode(in, host.next(it)),
        .repl = try host.decode(in, host.next(it)),
    };

    _ = h.pattern.reset(.retain_capacity);
    const res = try engine.Compile(h.pattern.allocator(), op.pattern, op.loc, op.flags);
    if (res[1].Code != 0) {
        try w.print("B {s} {d} 0 0\n", .{ name, res[1].Code });
        return;
    }
    op.re = res[0];

    // A compile iteration resets the pattern arena, which drops the expression compiled above.
    // The compile kind never reads it, so that is harmless.
    const arena = if (kind == .compile) h.pattern else h.scratch;

    var counting: Counting = .{ .child = arena.allocator() };
    try runPass(&op, arena, counting.allocator(), iters);
    const per_op_bytes = if (iters == 0) 0 else counting.bytes / iters;
    const per_op_allocs = if (iters == 0) 0 else counting.allocs / iters;
    try w.print("B {s} 0 {d} {d}", .{ name, per_op_bytes, per_op_allocs });

    const mem = arena.allocator();
    var rep: usize = 0;
    while (rep < reps) : (rep += 1) {
        const start = Io.Timestamp.now(io, .awake);
        try runPass(&op, arena, mem, iters);
        const elapsed = start.durationTo(Io.Timestamp.now(io, .awake));
        try w.print(" {d}", .{@as(i64, @intCast(elapsed.nanoseconds))});
    }
    try w.writeAll("\n");
}

fn handle(h: *Host, io: Io, w: *Io.Writer, line: []const u8) !void {
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
        'B' => try bench(h, io, w, &it),
        else => @panic("unknown bench command"),
    }
}

pub fn main(init: std.process.Init) !void {
    const io = init.io;
    const gpa = std.heap.page_allocator;

    var arena_persistent = std.heap.ArenaAllocator.init(gpa);
    defer arena_persistent.deinit();
    var arena_input = std.heap.ArenaAllocator.init(gpa);
    defer arena_input.deinit();
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
        .input = &arena_input,
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
            try handle(&h, io, w, line);
            try w.flush();
        }
        if (ended) {
            break;
        }
    }
    try w.flush();
}
