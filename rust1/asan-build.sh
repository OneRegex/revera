#!/bin/sh
# Build every binary of this crate with AddressSanitizer.
# The sanitizer needs the nightly toolchain and an explicit target triple, which rustc reports itself.
# The binaries land in target/asan-bin/.

set -e
cd "$(dirname "$0")"
RUSTFLAGS=-Zsanitizer=address cargo +nightly build --target "$(rustc -vV | sed -n 's/^host: //p')" --target-dir target/asan -Z unstable-options --artifact-dir target/asan-bin
