// Probe runner for the Zig instantiation. It prints the same
// lines as go1/cmd/proberef; go1/cmd/probecheck diffs them.

const std = @import("std");
const Io = std.Io;
const p = @import("probe_engine.zig");
const vg = @import("vg.zig");

const minI64: i64 = -9223372036854775808;

fn mk(a: []const u8, b: []const u8, n: i32) p.Tagged {
    return .{ .Tags = .{ vg.str(a), vg.str(b) }, .N = n };
}

fn eq(x: p.Tagged, y: p.Tagged) i64 {
    return @intFromBool(p.TaggedEq(x, y));
}

pub fn main(init: std.process.Init) !void {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const mem = arena.allocator();

    var outbuf: [1 << 14]u8 = undefined;
    var out_w: Io.File.Writer = .init(.stdout(), init.io, &outbuf);
    const w = &out_w.interface;

    for ([_][2]i64{
        .{ minI64, -1 }, .{ 7, -2 }, .{ -7, 2 }, .{ minI64, 1 }, .{ 1, minI64 },
    }) |ab| {
        const qr = p.DivMod(ab[0], ab[1]);
        try w.print("divmod {d} {d} = {d} {d}\n", .{ ab[0], ab[1], qr[0], qr[1] });
    }
    const qr32 = p.DivMod32(-2147483648, -1);
    try w.print("divmod32 = {d} {d}\n", .{ qr32[0], qr32[1] });
    const qr32b = p.DivMod32(9, -4);
    try w.print("divmod32b = {d} {d}\n", .{ qr32b[0], qr32b[1] });
    try w.print("bytes = {d} {d}\n", .{ p.BytesProbe(mem, vg.lit("hello")), p.BytesProbe(mem, .{}) });
    var c1: p.Counter = .{};
    try w.print("range = {d}\n", .{p.RangeProbe(mem, &c1)});
    try w.print("rangeval = {d}\n", .{p.RangeValProbe(vg.sliceOf(mem, i32, &[_]i32{ 3, 5, 7 }))});
    try w.print("rangeint = {d}\n", .{p.RangeIntProbe(5)});
    try w.print("partial = {d}\n", .{p.PartialArray()});
    try w.print("tagged = {d} {d} {d}\n", .{
        eq(mk("a", "b", 1), mk("a", "b", 1)),
        eq(mk("a", "b", 1), mk("a", "c", 1)),
        eq(mk("a", "b", 1), mk("a", "b", 2)),
    });
    var c2: p.Counter = .{};
    try w.print("orderargs = {d}\n", .{p.OrderArgs(mem, &c2)});
    var c3: p.Counter = .{};
    try w.print("orderbinary = {d}\n", .{p.OrderBinary(mem, &c3)});
    var c4: p.Counter = .{};
    try w.print("orderindex = {d}\n", .{p.OrderIndex(mem, &c4)});
    try w.print("spare = {d}\n", .{p.SpareProbe(mem)});
    try w.print("nil = {d}\n", .{p.NilProbe(mem)});
    try w.print("wrap = {d} {d}\n", .{ p.WrapProbe(minI64, 3), p.WrapProbe(7, -9) });
    try w.print("narrow32 = {d} {d}\n", .{ p.Narrow32(-2147483648, -1), p.Narrow32(-17, 5) });
    try w.print("wrapu8 = {d}\n", .{p.WrapU8(3, 200)});
    try w.print("andnot = {d}\n", .{p.AndNotProbe(0xF0F0F0F0, 0xFF00FF00)});
    try w.print("shift = {d}\n", .{p.ShiftProbe(0x8000000000000001, 7)});
    try w.print("conv = {d} {d}\n", .{ p.ConvProbe(-99), p.ConvProbe(300) });
    try w.print("subwrite = {d}\n", .{p.SubWrite(mem, 4)});
    var c5: p.Counter = .{};
    try w.print("andnotorder = {d}\n", .{p.AndNotOrder(mem, &c5)});
    try w.print("zeroarray = {d}\n", .{p.ZeroArray()});
    try w.print("makeu64 = {d}\n", .{p.MakeU64(mem, 6)});
    var c6: p.Counter = .{};
    try w.print("pickarray = {d}\n", .{p.PickArray(mem, &c6)});
    try w.flush();
}
