const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{ .preferred_optimize_mode = .ReleaseSafe });

    // The public module.
    // A package that depends on this one imports it as @import("revera").
    _ = b.addModule("revera", .{
        .root_source_file = b.path("src/revera.zig"),
        .target = target,
        .optimize = optimize,
    });

    // The driver, probe, bench and fuzzcase tools live in tools/, outside the published package.
    // `zig build --build-file tools/build.zig` builds them from a checkout of the repository.

    const tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/revera_test.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });

    // The fuzz test runs its seed corpus under `zig build test`.
    // `zig build test --fuzz` turns the same binary into a fuzzer.
    const fuzz_tests = b.addTest(.{
        .name = "fuzz",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/fuzz.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });

    const test_step = b.step("test", "Run the public API tests and the fuzz seed corpus");
    test_step.dependOn(&b.addRunArtifact(tests).step);
    test_step.dependOn(&b.addRunArtifact(fuzz_tests).step);
}
