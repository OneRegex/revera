//! POSIX.1-2024 extended regular expressions.
//!
//! This module is the public surface of the revera engine.
//! The engine itself, in engine.zig, is generated from a Vego program.
//! It speaks in arenas, raw views and numeric flags.
//! Nothing here needs those, but engine.zig stays reachable for a caller who wants the execution flags of regexec().
//!
//!     var re = try revera.Regex.compile(gpa, "([a-z]+)([0-9]*)", .{});
//!     defer re.deinit();
//!
//!     var caps = (try re.captures("__abc12__")).?;
//!     defer caps.deinit();
//!     std.debug.print("{s}\n", .{caps.get(1).?.text()});
//!
//! Patterns and subjects are UTF-8.
//! The language is the POSIX ERE language: leftmost-longest matching, no backreferences, and no Perl escapes.
//! Bracket expressions read their character classes, collating elements and equivalence classes from a Locale.
//! The default locale is POSIX.

const std = @import("std");
const engine = @import("engine.zig");
const vg = @import("vg.zig");

const Allocator = std.mem.Allocator;

/// The CLDR locale blob compiled into this module.
pub const embedded_locale_data = @embedFile("data.bin");

/// The largest interval count a pattern may ask for, as in a{0,255}.
pub const dup_max: u32 = engine.dupMax;

/// Every way compilation or a search can fail.
/// The names follow the <regex.h> error constants.
pub const Error = error{
    /// The pattern is not a valid extended regular expression.
    InvalidPattern,
    /// A [[.x.]] reference names no collating element.
    InvalidCollatingElement,
    /// A [[:x:]] reference names no character class.
    InvalidCharacterClass,
    /// The pattern ends with a backslash.
    InvalidEscape,
    /// A backreference, which the ERE language does not have.
    InvalidBackReference,
    /// A bracket expression is not closed.
    UnbalancedBracket,
    /// A parenthesis is not closed.
    UnbalancedParenthesis,
    /// An interval brace is not closed.
    UnbalancedBrace,
    /// The interval content is not a valid count or count range.
    InvalidInterval,
    /// A range like [z-a] runs backwards, or its endpoint is not a single character.
    InvalidRange,
    /// The work needed passed a capacity limit.
    OutOfCapacity,
    /// A repetition operator has no operand to repeat.
    MissingRepeatOperand,
    /// The expression was compiled with no_captures, and the call needs match offsets.
    NoCaptures,
    /// A code this version of the module does not name.
    UnknownFailure,
};

fn errorOf(code: i32) Error {
    return switch (code) {
        engine.ErrBadPat => error.InvalidPattern,
        engine.ErrECollate => error.InvalidCollatingElement,
        engine.ErrECType => error.InvalidCharacterClass,
        engine.ErrEEscape => error.InvalidEscape,
        engine.ErrESubReg => error.InvalidBackReference,
        engine.ErrEBrack => error.UnbalancedBracket,
        engine.ErrEParen => error.UnbalancedParenthesis,
        engine.ErrEBrace => error.UnbalancedBrace,
        engine.ErrBadBR => error.InvalidInterval,
        engine.ErrERange => error.InvalidRange,
        engine.ErrESpace => error.OutOfCapacity,
        engine.ErrBadRpt => error.MissingRepeatOperand,
        engine.ErrENoSub => error.NoCaptures,
        else => error.UnknownFailure,
    };
}

/// One matched span of a subject.
/// It borrows the subject, so it stays valid only as long as the subject does.
pub const Match = struct {
    subject: []const u8,
    start: usize,
    end: usize,

    /// Returns the matched bytes.
    pub fn text(self: Match) []const u8 {
        return self.subject[self.start..self.end];
    }

    /// Returns the length of the match in bytes.
    pub fn len(self: Match) usize {
        return self.end - self.start;
    }

    /// Reports whether the match is the null string.
    pub fn isEmpty(self: Match) bool {
        return self.start == self.end;
    }
};

/// One match and the spans of its capturing groups.
///
/// Group 0 is the whole match.
/// A group that took no part in the match reads as null.
/// The group list is owned, so every Captures value needs deinit.
pub const Captures = struct {
    gpa: Allocator,
    groups: []const ?Match,

    pub fn deinit(self: *Captures) void {
        self.gpa.free(self.groups);
        self.* = undefined;
    }

    /// Returns group i, or null when it took no part in the match or does not exist.
    pub fn get(self: Captures, i: usize) ?Match {
        if (i >= self.groups.len) {
            return null;
        }
        return self.groups[i];
    }

    /// Returns the number of groups, counting the whole match.
    pub fn len(self: Captures) usize {
        return self.groups.len;
    }
};

/// A locale: the source of character classes, case folding, collating elements and equivalence classes.
pub const Locale = struct {
    inner: engine.Locale,

    /// Returns the POSIX locale, also called the C locale.
    pub fn posix() Locale {
        return .{ .inner = engine.LocalePOSIX() };
    }

    /// Resolves a CLDR locale name against the embedded data, for example Locale.open(gpa, "cs", "").
    /// An empty collation type takes the standard collation of the locale.
    /// The result is null when the name or the collation type is unknown.
    ///
    /// The allocator backs the lookup only.
    /// The locale it returns reads the embedded data and owns nothing.
    pub fn open(gpa: Allocator, name: []const u8, collation_type: []const u8) Allocator.Error!?Locale {
        var arena = std.heap.ArenaAllocator.init(gpa);
        defer arena.deinit();
        const res = try engine.LocaleOpen(
            arena.allocator(),
            vg.str(embedded_locale_data),
            vg.str(name),
            vg.str(collation_type),
        );
        if (!res[1]) {
            return null;
        }
        return .{ .inner = res[0] };
    }

    /// Returns every locale name the embedded data carries.
    /// The names point into that data, so the caller frees only the outer slice.
    pub fn names(gpa: Allocator) ![]const []const u8 {
        var res = engine.LocaleLoad(vg.str(embedded_locale_data));
        if (!res[1]) {
            return &.{};
        }
        const count: usize = @intCast(engine.LocaleCount(&res[0]));
        const out = try gpa.alloc([]const u8, count);
        for (out, 0..) |*slot, i| {
            slot.* = engine.LocaleName(&res[0], @intCast(i)).bytes();
        }
        return out;
    }
};

/// How to compile a pattern.
pub const Options = struct {
    /// Matches upper and lower case alike, like REG_ICASE.
    case_insensitive: bool = false,
    /// Gives ^ and $ their line meaning, like REG_NEWLINE.
    /// It also stops dot and negated brackets on a newline.
    newline_sensitive: bool = false,
    /// Compiles for a yes-or-no answer only, like REG_NOSUB.
    /// isMatch still works, and every other search reports error.NoCaptures.
    no_captures: bool = false,
    /// Makes every duplication prefer the shortest repetition.
    /// A repetition modifier reverses one duplication back.
    shortest_match: bool = false,
    /// The locale the bracket expressions read.
    /// Null means POSIX.
    locale: ?Locale = null,
    /// Receives the byte offset in the pattern where compilation stopped.
    /// Some failures have no position, and those leave the slot as the caller set it.
    error_position: ?*usize = null,

    fn flags(self: Options) u32 {
        var f: u32 = 0;
        if (self.case_insensitive) f |= engine.FlagICase;
        if (self.newline_sensitive) f |= engine.FlagNewline;
        if (self.no_captures) f |= engine.FlagNoSub;
        if (self.shortest_match) f |= engine.FlagMinimal;
        return f;
    }
};

/// A compiled regular expression.
///
/// A search takes a const pointer and keeps no state between calls.
/// One Regex therefore serves any number of threads.
/// The allocator it was compiled with must be thread safe, because every search takes its scratch memory from that allocator.
pub const Regex = struct {
    /// The arena owns the compiled program.
    /// compile writes it once, and every search only reads it.
    /// The arena also carries the allocator it was built from, which every search borrows.
    arena: std.heap.ArenaAllocator,
    inner: engine.Regexp,
    groups: usize,

    /// Compiles a pattern.
    /// The Regex owns memory until deinit.
    pub fn compile(gpa: Allocator, pattern: []const u8, opts: Options) (Error || Allocator.Error)!Regex {
        var arena = std.heap.ArenaAllocator.init(gpa);
        errdefer arena.deinit();
        const mem = arena.allocator();

        // The pattern goes into the arena first, so the caller may compile from a temporary and drop it.
        const owned = try vg.bytesFromStr(mem, vg.str(pattern));
        const loc = opts.locale orelse Locale.posix();
        var res = try engine.Compile(mem, vg.str(owned.items()), loc.inner, opts.flags());
        if (res[1].Code != engine.ErrNone) {
            if (opts.error_position) |slot| {
                if (res[1].Pos >= 0) {
                    slot.* = @intCast(res[1].Pos);
                }
            }
            return errorOf(res[1].Code);
        }
        return .{
            .arena = arena,
            .inner = res[0],
            .groups = @intCast(engine.NumSub(&res[0]) + 1),
        };
    }

    pub fn deinit(self: *Regex) void {
        self.arena.deinit();
        self.* = undefined;
    }

    // backing is the allocator compile was given.
    // Every search borrows it for scratch memory.
    fn backing(self: *const Regex) Allocator {
        return self.arena.child_allocator;
    }

    /// Returns the number of groups a search reports, counting the whole match.
    /// It is one more than the number of parenthesized subexpressions.
    pub fn groupCount(self: *const Regex) usize {
        return self.groups;
    }

    /// Reports whether the expression matches anywhere in subject.
    pub fn isMatch(self: *const Regex, subject: []const u8) (Error || Allocator.Error)!bool {
        var scratch = std.heap.ArenaAllocator.init(self.backing());
        defer scratch.deinit();
        return try self.exec(scratch.allocator(), subject, .{});
    }

    /// Returns the leftmost-longest match, or null when there is none.
    pub fn find(self: *const Regex, subject: []const u8) (Error || Allocator.Error)!?Match {
        try self.refuseWithoutCaptures();
        var scratch = std.heap.ArenaAllocator.init(self.backing());
        defer scratch.deinit();
        const pmatch = try vg.make(scratch.allocator(), engine.Match, 1);
        if (!try self.exec(scratch.allocator(), subject, pmatch)) {
            return null;
        }
        return spanOf(subject, pmatch.at(0).*).?;
    }

    /// Returns the leftmost-longest match with its groups, or null when there is none.
    /// The result owns memory until deinit.
    pub fn captures(
        self: *const Regex,
        subject: []const u8,
    ) (Error || Allocator.Error)!?Captures {
        try self.refuseWithoutCaptures();
        var scratch = std.heap.ArenaAllocator.init(self.backing());
        defer scratch.deinit();
        const pmatch = try vg.make(scratch.allocator(), engine.Match, @intCast(self.groups));
        if (!try self.exec(scratch.allocator(), subject, pmatch)) {
            return null;
        }
        return try self.collect(subject, pmatch);
    }

    /// Returns an iterator over the non-overlapping matches, left to right.
    /// The Regex and the subject must both outlive the iterator.
    pub fn matches(self: *const Regex, subject: []const u8) Error!MatchIterator {
        return .{ .scan = try Scan.init(self, subject) };
    }

    /// Returns an iterator over the non-overlapping matches with their groups.
    /// Every value it yields owns memory until deinit.
    /// The Regex and the subject must both outlive the iterator.
    pub fn captureMatches(self: *const Regex, subject: []const u8) Error!CaptureIterator {
        return .{ .scan = try Scan.init(self, subject) };
    }

    /// Returns subject with every non-overlapping match replaced, like the sed command s///g.
    /// The caller frees the result.
    ///
    /// In replacement, & stands for the whole match and \1 through \9 for one group.
    /// A backslash escapes the next character, so \& and \\ are literal.
    pub fn replaceAll(
        self: *const Regex,
        subject: []const u8,
        replacement: []const u8,
    ) (Error || Allocator.Error)![]u8 {
        return self.replaceBounded(subject, replacement, -1);
    }

    /// Returns subject with at most limit matches replaced.
    /// The rest of the subject stays as it is.
    pub fn replaceFirstN(
        self: *const Regex,
        subject: []const u8,
        replacement: []const u8,
        limit: usize,
    ) (Error || Allocator.Error)![]u8 {
        return self.replaceBounded(subject, replacement, std.math.lossyCast(i64, limit));
    }

    /// Returns what one search can cost on a subject of at most max_input bytes.
    /// An application compares the figures against its budget.
    /// It can then refuse the expression before the expression ever runs.
    pub fn contract(self: *const Regex, max_input: usize) Contract {
        var re = self.inner;
        var c = engine.ContractFor(&re, std.math.lossyCast(i64, max_input));
        return .{
            .max_input = @intCast(c.MaxInput),
            .heap_bytes = @intCast(engine.ContractHeapBytes(&c)),
            .stack_bytes = @intCast(engine.ContractStackBytes(&c)),
            .steps = @intCast(engine.ContractSteps(&c)),
            .matcher = backendOf(c.Matcher),
            .one_pass = if (c.HasOnePass) backendOf(c.OnePass) else null,
            .solver = if (c.HasSolver) backendOf(c.Solver) else null,
        };
    }

    fn refuseWithoutCaptures(self: *const Regex) Error!void {
        if (self.inner.flags & engine.FlagNoSub != 0) {
            return error.NoCaptures;
        }
    }

    fn exec(
        self: *const Regex,
        mem: Allocator,
        subject: []const u8,
        pmatch: vg.Slice(engine.Match),
    ) (Error || Allocator.Error)!bool {
        var re = self.inner;
        const res = try engine.Exec(mem, &re, vg.str(subject), pmatch, 0);
        if (res[1].Code != engine.ErrNone) {
            return errorOf(res[1].Code);
        }
        return res[0];
    }

    fn collect(
        self: *const Regex,
        subject: []const u8,
        pmatch: vg.Slice(engine.Match),
    ) Allocator.Error!Captures {
        const groups = try self.backing().alloc(?Match, @intCast(pmatch.len));
        for (groups, pmatch.items()) |*slot, m| {
            slot.* = spanOf(subject, m);
        }
        return .{ .gpa = self.backing(), .groups = groups };
    }

    fn replaceBounded(
        self: *const Regex,
        subject: []const u8,
        replacement: []const u8,
        limit: i64,
    ) (Error || Allocator.Error)![]u8 {
        var scratch = std.heap.ArenaAllocator.init(self.backing());
        defer scratch.deinit();
        var re = self.inner;
        const res = try engine.ReplaceAll(
            scratch.allocator(),
            &re,
            vg.str(subject),
            vg.str(replacement),
            limit,
            0,
        );
        if (res[1].Code != engine.ErrNone) {
            return errorOf(res[1].Code);
        }
        return self.backing().dupe(u8, res[0].bytes());
    }
};

/// The non-overlapping matches of one search, from Regex.matches.
pub const MatchIterator = struct {
    scan: Scan,

    /// Returns the next match, or null at the end of the subject.
    pub fn next(self: *MatchIterator) (Error || Allocator.Error)!?Match {
        var scratch = std.heap.ArenaAllocator.init(self.scan.re.backing());
        defer scratch.deinit();
        const pmatch = (try self.scan.step(scratch.allocator())) orelse return null;
        return spanOf(self.scan.subject, pmatch.at(0).*).?;
    }
};

/// The non-overlapping matches of one search with their groups, from Regex.captureMatches.
pub const CaptureIterator = struct {
    scan: Scan,

    /// Returns the next match, or null at the end of the subject.
    /// Every value it returns needs deinit.
    pub fn next(self: *CaptureIterator) (Error || Allocator.Error)!?Captures {
        var scratch = std.heap.ArenaAllocator.init(self.scan.re.backing());
        defer scratch.deinit();
        const pmatch = (try self.scan.step(scratch.allocator())) orelse return null;
        return try self.scan.re.collect(self.scan.subject, pmatch);
    }
};

// Scan drives one iteration over the non-overlapping matches, and both iterators wrap it.
// One step fills a slice of the caller's arena.
// The caller can then free the arena as soon as it has copied the offsets out.
const Scan = struct {
    re: *const Regex,
    subject: []const u8,
    // The engine takes a mutable pointer to the compiled program, so a scan walks a copy of the header.
    // One copy serves every step of the scan.
    walk: engine.Regexp,
    iter: engine.MatchIter,
    done: bool,

    fn init(re: *const Regex, subject: []const u8) Error!Scan {
        var walk = re.inner;
        const res = engine.MatchIterInit(&walk, -1);
        if (res[1].Code != engine.ErrNone) {
            return errorOf(res[1].Code);
        }
        return .{ .re = re, .subject = subject, .walk = walk, .iter = res[0], .done = false };
    }

    fn step(self: *Scan, mem: Allocator) (Error || Allocator.Error)!?vg.Slice(engine.Match) {
        if (self.done) {
            return null;
        }
        const pmatch = try vg.make(mem, engine.Match, @intCast(self.re.groups));
        const res = try engine.MatchIterNext(mem, &self.walk, &self.iter, vg.str(self.subject), 0, pmatch);
        if (res[1].Code != engine.ErrNone) {
            self.done = true;
            return errorOf(res[1].Code);
        }
        if (!res[0]) {
            self.done = true;
            return null;
        }
        return pmatch;
    }
};

/// What one backend of one search can use.
pub const BackendContract = struct {
    /// The bound on explicit heap allocation, in bytes.
    heap_bytes: u64,
    /// The estimate of the deepest call stack, in bytes.
    stack_bytes: u64,
    /// The bound on abstract operations.
    /// These are unit-cost operations, not nanoseconds.
    steps: u64,
};

/// What one search can cost, from Regex.contract.
///
/// Every figure saturates at 1 << 62, which marks a bound too large to be useful.
pub const Contract = struct {
    /// The subject length the figures cover, in bytes.
    max_input: usize,
    /// The heap bound of a whole search, in bytes.
    heap_bytes: u64,
    /// The stack estimate of a whole search, in bytes.
    stack_bytes: u64,
    /// The step bound of a whole search.
    steps: u64,
    /// The figures of the automaton, which every search runs.
    matcher: BackendContract,
    /// The figures of the one-pass capture walk.
    /// This is the only capture backend when compilation proved that every span has one parse.
    one_pass: ?BackendContract,
    /// The figures of the memoized capture search for an expression that is not one-pass.
    solver: ?BackendContract,
};

fn backendOf(b: engine.BackendContract) BackendContract {
    return .{
        .heap_bytes = @intCast(b.HeapBytes),
        .stack_bytes = @intCast(b.StackBytes),
        .steps = @intCast(b.Steps),
    };
}

fn spanOf(subject: []const u8, m: engine.Match) ?Match {
    if (m.So < 0) {
        return null;
    }
    return .{ .subject = subject, .start = @intCast(m.So), .end = @intCast(m.Eo) };
}
