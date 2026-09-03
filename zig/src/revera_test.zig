const std = @import("std");
const revera = @import("revera.zig");

const testing = std.testing;
const gpa = testing.allocator;

test "find and captures" {
    var re = try revera.Regex.compile(gpa, "([a-z]+)([0-9]*)", .{});
    defer re.deinit();

    try testing.expectEqual(3, re.groupCount());
    try testing.expect(try re.isMatch("__abc12__"));

    const m = (try re.find("__abc12__")).?;
    try testing.expectEqual(2, m.start);
    try testing.expectEqual(7, m.end);
    try testing.expectEqualStrings("abc12", m.text());
    try testing.expectEqual(5, m.len());
    try testing.expect(!m.isEmpty());

    var caps = (try re.captures("__abc12__")).?;
    defer caps.deinit();
    try testing.expectEqual(3, caps.len());
    try testing.expectEqualStrings("abc", caps.get(1).?.text());
    try testing.expectEqualStrings("12", caps.get(2).?.text());
}

test "a group that took no part reads as null" {
    var re = try revera.Regex.compile(gpa, "(a)|(b)", .{});
    defer re.deinit();

    var caps = (try re.captures("a")).?;
    defer caps.deinit();
    try testing.expect(caps.get(1) != null);
    try testing.expect(caps.get(2) == null);
    try testing.expect(caps.get(9) == null);
}

test "no match reads as null" {
    var re = try revera.Regex.compile(gpa, "z+", .{});
    defer re.deinit();

    try testing.expect(!try re.isMatch("abc"));
    try testing.expect((try re.find("abc")) == null);
    try testing.expect((try re.captures("abc")) == null);

    var it = try re.matches("abc");
    try testing.expect((try it.next()) == null);
}

test "iterators walk every match" {
    var re = try revera.Regex.compile(gpa, "(a+)(b*)", .{});
    defer re.deinit();

    const subject = "aab a aabbb";
    var found: std.ArrayList([]const u8) = .empty;
    defer found.deinit(gpa);
    var it = try re.matches(subject);
    while (try it.next()) |m| {
        try found.append(gpa, m.text());
    }
    try testing.expectEqual(3, found.items.len);
    try testing.expectEqualStrings("aab", found.items[0]);
    try testing.expectEqualStrings("a", found.items[1]);
    try testing.expectEqualStrings("aabbb", found.items[2]);

    var groups = try re.captureMatches("aab a");
    var count: usize = 0;
    while (try groups.next()) |c| {
        var caps = c;
        defer caps.deinit();
        try testing.expectEqual(3, caps.len());
        count += 1;
    }
    try testing.expectEqual(2, count);
}

test "replacement" {
    var re = try revera.Regex.compile(gpa, "(a+)(b*)", .{});
    defer re.deinit();

    const all = try re.replaceAll("xaabyy", "[&:\\2]");
    defer gpa.free(all);
    try testing.expectEqualStrings("x[aab:b]yy", all);

    const one = try re.replaceFirstN("aa bb aa", "X", 1);
    defer gpa.free(one);
    try testing.expectEqualStrings("X bb aa", one);
}

test "options" {
    var icase = try revera.Regex.compile(gpa, "ab+", .{ .case_insensitive = true });
    defer icase.deinit();
    try testing.expect(try icase.isMatch("ABBB"));

    var lines = try revera.Regex.compile(gpa, "^b", .{ .newline_sensitive = true });
    defer lines.deinit();
    try testing.expectEqual(2, (try lines.find("a\nbc")).?.start);

    var shortest = try revera.Regex.compile(gpa, "a+", .{ .shortest_match = true });
    defer shortest.deinit();
    try testing.expectEqual(1, (try shortest.find("aaa")).?.end);

    var plain = try revera.Regex.compile(gpa, "a+", .{ .no_captures = true });
    defer plain.deinit();
    try testing.expect(try plain.isMatch("baa"));
    try testing.expectError(error.NoCaptures, plain.find("baa"));
}

test "locales" {
    const cs = (try revera.Locale.open(gpa, "cs", "")).?;
    var re = try revera.Regex.compile(gpa, "[[.ch.]]", .{ .locale = cs });
    defer re.deinit();
    try testing.expect(try re.isMatch("ch"));
    try testing.expect(try revera.Locale.open(gpa, "xx-not-there", "") == null);

    const names = try revera.Locale.names(gpa);
    defer gpa.free(names);
    try testing.expect(names.len > 1000);
}

test "compile failure reports a position" {
    var at: usize = 0;
    const err = revera.Regex.compile(gpa, "a(", .{ .error_position = &at });
    try testing.expectError(error.InvalidPattern, err);
    try testing.expectEqual(2, at);

    try testing.expectError(
        error.InvalidCharacterClass,
        revera.Regex.compile(gpa, "[[:bogus:]]", .{}),
    );
}

test "contract grows with the input bound" {
    var re = try revera.Regex.compile(gpa, "(a|ab)(c|bcd)(d*)", .{});
    defer re.deinit();

    const big = re.contract(1 << 12);
    try testing.expectEqual(1 << 12, big.max_input);
    try testing.expect(big.heap_bytes > 0);
    try testing.expect(big.stack_bytes > 0);
    try testing.expect(big.one_pass == null);
    try testing.expect(big.solver != null);
    try testing.expect(big.matcher.steps > 0);
    try testing.expect(re.contract(16).steps < big.steps);

    // An absurd bound clamps to the subject limit of the engine.
    try testing.expectEqual((1 << 31) - 1, re.contract(std.math.maxInt(usize)).max_input);

    var simple = try revera.Regex.compile(gpa, "(abc+)", .{});
    defer simple.deinit();
    const one_pass = simple.contract(1000);
    try testing.expect(one_pass.one_pass != null);
    try testing.expect(one_pass.solver == null);
    try testing.expectEqual(37_757, one_pass.heap_bytes);
    try testing.expectEqual(6_144, one_pass.stack_bytes);
    try testing.expectEqual(937_980, one_pass.steps);
}

test "one expression serves several threads" {
    var re = try revera.Regex.compile(gpa, "[0-9]+", .{});
    defer re.deinit();

    const search = struct {
        fn run(r: *const revera.Regex) !void {
            for (0..200) |_| {
                const m = (try r.find("ab 1234 cd")).?;
                try testing.expectEqualStrings("1234", m.text());
            }
        }
    }.run;

    var threads: [4]std.Thread = undefined;
    for (&threads) |*t| {
        t.* = try std.Thread.spawn(.{}, search, .{&re});
    }
    for (threads) |t| {
        t.join();
    }
}
