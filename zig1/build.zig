const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{ .preferred_optimize_mode = .ReleaseSafe });

    // The public module. A package that depends on this one imports
    // it as @import("revera").
    _ = b.addModule("revera", .{
        .root_source_file = b.path("src/revera.zig"),
        .target = target,
        .optimize = optimize,
    });

    const exe = b.addExecutable(.{
        .name = "driver",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/main.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    b.installArtifact(exe);

    const probe = b.addExecutable(.{
        .name = "probe",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/probe_main.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    b.installArtifact(probe);

    const tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/revera_test.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    const test_step = b.step("test", "Run the public API tests");
    test_step.dependOn(&b.addRunArtifact(tests).step);
}
