#!/bin/sh
# Copy the license files of the repository root into every package that ships on its own.
# Cargo, the Zig package manager and the native archive all read from their own directory,
# and the Go module of go/ needs the data license next to data.bin.
# The dist command checks that the copies are current.

set -eu
cd "$(dirname "$0")/.."

for dir in go rust zig native; do
    mkdir -p "$dir/LICENSES"
    cp LICENSES/Unicode-3.0.txt "$dir/LICENSES/Unicode-3.0.txt"
    if [ -f LICENSE ]; then
        cp LICENSE "$dir/LICENSE"
    fi
done
if [ ! -f LICENSE ]; then
    echo "sync-licenses: no LICENSE at the repository root yet; only the data license was copied" >&2
fi
