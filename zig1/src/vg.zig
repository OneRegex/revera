// Minimal runtime for the generated Vego engine. It supplies what
// the Go runtime supplied implicitly: growable buffers, immutable
// string views, conversions with Go semantics, and memory.
//
// Memory is explicit. Every generated function that allocates
// takes an allocator as its first parameter, so the runtime holds
// no state at all. The host owns the arenas and decides which one
// backs each engine call; two threads with separate arenas never
// share anything mutable.

const std = @import("std");

pub const Allocator = std.mem.Allocator;

const zeroOf = std.mem.zeroes;

// Str is an immutable byte view, the translation of a Go string.
// The zero value is the empty string.
pub const Str = struct {
    p: ?[*]const u8 = null,
    len: i64 = 0,

    pub fn byte(self: Str, i: i64) u8 {
        std.debug.assert(i >= 0 and i < self.len);
        return self.p.?[@intCast(i)];
    }

    pub fn sub(self: Str, lo: i64, hi: i64) Str {
        std.debug.assert(0 <= lo and lo <= hi and hi <= self.len);
        if (self.p) |p| {
            return .{ .p = p + @as(usize, @intCast(lo)), .len = hi - lo };
        }
        return .{};
    }

    pub fn tail(self: Str, lo: i64) Str {
        return self.sub(lo, self.len);
    }

    pub fn head(self: Str, hi: i64) Str {
        return self.sub(0, hi);
    }

    pub fn bytes(self: Str) []const u8 {
        if (self.p) |p| {
            return p[0..@intCast(self.len)];
        }
        return &.{};
    }
};

pub fn lit(comptime s: []const u8) Str {
    return .{ .p = s.ptr, .len = s.len };
}

// str wraps runtime bytes as a Str without copying. The caller
// keeps the bytes alive.
pub fn str(s: []const u8) Str {
    return .{ .p = s.ptr, .len = @intCast(s.len) };
}

pub fn streq(a: Str, b: Str) bool {
    return std.mem.eql(u8, a.bytes(), b.bytes());
}

pub fn strcmp3(a: Str, b: Str) i32 {
    return switch (std.mem.order(u8, a.bytes(), b.bytes())) {
        .lt => -1,
        .eq => 0,
        .gt => 1,
    };
}

// Slice is a Go slice header: pointer, length, capacity.
// Assignment copies the header and shares the buffer, exactly like
// Go. A null pointer is the nil slice; every allocation, even a
// zero-length one, produces a non-null pointer.
pub fn Slice(comptime T: type) type {
    return struct {
        p: ?[*]T = null,
        len: i64 = 0,
        cap: i64 = 0,

        const Self = @This();

        pub fn at(self: Self, i: i64) *T {
            std.debug.assert(i >= 0 and i < self.len);
            return &self.p.?[@intCast(i)];
        }

        pub fn sub(self: Self, lo: i64, hi: i64) Self {
            std.debug.assert(0 <= lo and lo <= hi and hi <= self.cap);
            if (self.p) |p| {
                return .{
                    .p = p + @as(usize, @intCast(lo)),
                    .len = hi - lo,
                    .cap = self.cap - lo,
                };
            }
            return .{};
        }

        pub fn tail(self: Self, lo: i64) Self {
            return self.sub(lo, self.len);
        }

        pub fn head(self: Self, hi: i64) Self {
            return self.sub(0, hi);
        }

        pub fn items(self: Self) []T {
            if (self.p) |p| {
                return p[0..@intCast(self.len)];
            }
            return &.{};
        }
    };
}

fn allocElems(gpa: Allocator, comptime T: type, n: i64) [*]T {
    const count: usize = @intCast(@max(n, 1));
    const buf = gpa.alloc(T, count) catch @panic("out of memory");
    return buf.ptr;
}

pub fn make(gpa: Allocator, comptime T: type, n: i64) Slice(T) {
    return makeCap(gpa, T, n, n);
}

pub fn makeCap(gpa: Allocator, comptime T: type, n: i64, c: i64) Slice(T) {
    std.debug.assert(0 <= n and n <= c);
    const p = allocElems(gpa, T, c);
    @memset(p[0..@intCast(c)], zeroOf(T));
    return .{ .p = p, .len = n, .cap = c };
}

fn grow(gpa: Allocator, comptime T: type, s: Slice(T), need: i64) Slice(T) {
    var newcap: i64 = @max(s.cap * 2, 8);
    if (newcap < need) {
        newcap = need;
    }
    // The spare region must read as zero: Go allocates zeroed
    // memory, and extending a slice inside its capacity exposes
    // that memory. The prefix gets the live elements instead.
    const p = allocElems(gpa, T, newcap);
    const n: usize = @intCast(s.len);
    @memset(p[n..@intCast(newcap)], zeroOf(T));
    if (s.p) |old| {
        @memcpy(p[0..n], old[0..n]);
    }
    return .{ .p = p, .len = s.len, .cap = newcap };
}

pub fn append(gpa: Allocator, comptime T: type, s: Slice(T), v: T) Slice(T) {
    var out = s;
    if (out.len == out.cap) {
        out = grow(gpa, T, out, out.len + 1);
    }
    out.p.?[@intCast(out.len)] = v;
    out.len += 1;
    return out;
}

pub fn appendSlice(gpa: Allocator, comptime T: type, s: Slice(T), more: Slice(T)) Slice(T) {
    var out = s;
    if (out.len + more.len > out.cap) {
        out = grow(gpa, T, out, out.len + more.len);
    }
    const n: usize = @intCast(more.len);
    if (n > 0) {
        // The source may alias the old buffer; the old buffer is
        // still intact after a grow, so a plain copy is right.
        @memmove(out.p.?[@intCast(out.len)..][0..n], more.p.?[0..n]);
    }
    out.len += more.len;
    return out;
}

pub fn appendStr(gpa: Allocator, s: Slice(u8), more: Str) Slice(u8) {
    var out = s;
    if (out.len + more.len > out.cap) {
        out = grow(gpa, u8, out, out.len + more.len);
    }
    const n: usize = @intCast(more.len);
    if (n > 0) {
        @memmove(out.p.?[@intCast(out.len)..][0..n], more.p.?[0..n]);
    }
    out.len += more.len;
    return out;
}

pub fn copy(comptime T: type, dst: Slice(T), src: Slice(T)) i64 {
    const n = @min(dst.len, src.len);
    if (n > 0) {
        const un: usize = @intCast(n);
        @memmove(dst.p.?[0..un], src.p.?[0..un]);
    }
    return n;
}

pub fn copyStr(dst: Slice(u8), src: Str) i64 {
    const n = @min(dst.len, src.len);
    if (n > 0) {
        const un: usize = @intCast(n);
        @memmove(dst.p.?[0..un], src.p.?[0..un]);
    }
    return n;
}

pub fn strFromBytes(gpa: Allocator, b: Slice(u8)) Str {
    const n: usize = @intCast(b.len);
    const p = allocElems(gpa, u8, b.len);
    if (n > 0) {
        @memcpy(p[0..n], b.p.?[0..n]);
    }
    return .{ .p = p, .len = b.len };
}

pub fn arrSlice(comptime T: type, arr: anytype, lo: i64, hi: i64) Slice(T) {
    const n: i64 = @intCast(arr.len);
    std.debug.assert(0 <= lo and lo <= hi and hi <= n);
    return .{
        .p = @as([*]T, @ptrCast(arr)) + @as(usize, @intCast(lo)),
        .len = hi - lo,
        .cap = n - lo,
    };
}

pub fn sliceOf(gpa: Allocator, comptime T: type, src: anytype) Slice(T) {
    const out = make(gpa, T, @intCast(src.len));
    @memcpy(out.p.?[0..src.len], src);
    return out;
}

// structEq compares two struct values field by field, the way Go
// compares comparable values. Str fields compare by content, and
// arrays compare element by element.
pub fn structEq(a: anytype, b: @TypeOf(a)) bool {
    const T = @TypeOf(a);
    if (T == Str) {
        return streq(a, b);
    }
    switch (@typeInfo(T)) {
        .@"struct" => |info| {
            inline for (info.field_names) |name| {
                if (!structEq(@field(a, name), @field(b, name))) {
                    return false;
                }
            }
            return true;
        },
        .array => {
            for (a, b) |x, y| {
                if (!structEq(x, y)) {
                    return false;
                }
            }
            return true;
        },
        else => return a == b,
    }
}

// divT and remT are Go's truncating division and remainder. Go
// defines MinInt / -1 as MinInt (wrapping) and MinInt % -1 as 0;
// the plain Zig builtins trap on that pair.
pub fn divT(a: anytype, b: @TypeOf(a)) @TypeOf(a) {
    const T = @TypeOf(a);
    if (T != comptime_int and @typeInfo(T).int.signedness == .signed) {
        if (b == -1) {
            return 0 -% a;
        }
    }
    return @divTrunc(a, b);
}

pub fn remT(a: anytype, b: @TypeOf(a)) @TypeOf(a) {
    const T = @TypeOf(a);
    if (T != comptime_int and @typeInfo(T).int.signedness == .signed) {
        if (b == -1) {
            return 0;
        }
    }
    return @rem(a, b);
}

// bytesFromStr copies a string into a fresh mutable byte buffer,
// the []uint8(s) conversion.
pub fn bytesFromStr(gpa: Allocator, s: Str) Slice(u8) {
    const out = make(gpa, u8, s.len);
    _ = copyStr(out, s);
    return out;
}

// cv converts between integer types with Go semantics: widen with
// the sign of the source, then truncate to the target width and
// reinterpret.
pub inline fn cv(comptime B: type, x: anytype) B {
    const A = @TypeOf(x);
    if (A == comptime_int) {
        return x;
    }
    const ext = if (@typeInfo(A).int.signedness == .signed)
        @as(i64, x)
    else
        @as(u64, x);
    const bits: u64 = @bitCast(ext);
    const UB = @Int(.unsigned, @bitSizeOf(B));
    return @bitCast(@as(UB, @truncate(bits)));
}
