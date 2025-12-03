#!/usr/bin/env bash

set -euo pipefail

# Always work from the repo root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# Define paths to libraries (relative to repo root)
NIX_LIB="ffi/result/lib/libfirewood_ffi.a" # Default path for the nix build
CARGO_LIB="target/maxperf/libfirewood_ffi.a"

# Create temporary directory and ensure cleanup on exit
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

echo "Building with cargo (using nix dev shell)..."
nix develop ./ffi#default --command bash -c "cargo fetch --locked --verbose && cargo build-static-ffi"

echo "Building with nix..."
cd ffi && nix build .#firewood-ffi && cd ..

echo ""
echo "=== File Size Comparison ==="
ls -lh "$CARGO_LIB" "$NIX_LIB"

echo ""
echo "=== Symbol Count Comparison ==="
# Extract symbols to temporary files for comparison
nm "$NIX_LIB" | sort > "$TMPDIR/nix-symbols.txt"
nm "$CARGO_LIB" | sort > "$TMPDIR/cargo-symbols.txt"

NIX_SYMBOLS=$(wc -l < "$TMPDIR/nix-symbols.txt")
CARGO_SYMBOLS=$(wc -l < "$TMPDIR/cargo-symbols.txt")
echo "Nix build:   $NIX_SYMBOLS symbols"
echo "Cargo build: $CARGO_SYMBOLS symbols"
if [ "$NIX_SYMBOLS" -eq "$CARGO_SYMBOLS" ]; then
    echo "✅ Symbol counts are both $NIX_SYMBOLS"
else
    echo "❌ Symbol counts differ"
    echo ""
    echo "=== Symbol Differences ==="
    echo "Symbols only in Nix build:"
    # Show lines that exist in the old file (nix) but not in the new file (cargo)
    diff --unchanged-line-format="" --old-line-format="%L" --new-line-format="" "$TMPDIR/nix-symbols.txt" "$TMPDIR/cargo-symbols.txt" || true
    echo ""
    echo "Symbols only in Cargo build:"
    # Show lines that exist in the new file (cargo) but not in the old file (nix)
    diff --unchanged-line-format="" --old-line-format="" --new-line-format="%L" "$TMPDIR/nix-symbols.txt" "$TMPDIR/cargo-symbols.txt" || true
fi
