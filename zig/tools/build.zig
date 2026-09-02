const std = @import("std");

// The development tools of the Zig backend: the differential driver, the probe runner, the bench binary and the fuzz seed replayer.
// They are not part of the published package, so they have their own build file.
// Run `zig build --build-file tools/build.zig -Drelease -p zig-out` from the zig/ directory.
pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    // The package takes -Drelease, a boolean, so the tools take the same flag and pass it on.
    const release = b.option(bool, "release", "optimize for end users") orelse false;
    const optimize: std.builtin.OptimizeMode = if (release) .ReleaseSafe else .Debug;

    // The parent directory is the package; its sources are reached through the path dependency.
    const revera = b.dependency("revera", .{ .target = target, .release = release });

    const executables = [_]struct { []const u8, []const u8 }{
        .{ "driver", "src/main.zig" },
        .{ "probe", "src/probe_main.zig" },
        .{ "bench", "src/bench_main.zig" },
        .{ "fuzzcase", "src/fuzzcase_main.zig" },
    };
    for (executables) |entry| {
        const name, const source = entry;
        b.installArtifact(b.addExecutable(.{
            .name = name,
            .root_module = b.createModule(.{
                .root_source_file = revera.path(source),
                .target = target,
                .optimize = optimize,
            }),
        }));
    }
}
