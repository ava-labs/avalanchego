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

# Build serially with MAKEFLAGS='-j1' for consistency with the flake build
echo "Building with cargo (using nix dev shell)..."
nix develop ./ffi#default --command bash -c "export MAKEFLAGS='-j1' && cargo fetch --locked --verbose && cargo build-static-ffi"

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
    echo "✅ Symbol counts match"
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

echo ""
echo "=== Relocation Count Comparison ==="

# Determine os-specific reloc config
if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS
    RELOC_CMD="otool -rv"
    RELOC_PATTERN='[A-Z_]+_RELOC_[A-Z0-9_]+'
else
    # Linux
    RELOC_CMD="readelf -r"
    RELOC_PATTERN='R_[A-Z0-9_]+'
fi

$RELOC_CMD "$NIX_LIB" > "$TMPDIR/nix-relocs.txt"
$RELOC_CMD "$CARGO_LIB" > "$TMPDIR/cargo-relocs.txt"

NIX_RELOCS=$(wc -l < "$TMPDIR/nix-relocs.txt")
CARGO_RELOCS=$(wc -l < "$TMPDIR/cargo-relocs.txt")
echo "Nix build:   $NIX_RELOCS relocation entries"
echo "Cargo build: $CARGO_RELOCS relocation entries"
if [ "$NIX_RELOCS" -eq "$CARGO_RELOCS" ]; then
    echo "✅ Relocation counts match"
else
    echo "❌ Relocation counts differ"
fi

echo ""
echo "=== Relocation Type Comparison ==="

# Use grep with -E for better portability (avoid -P which isn't available on macOS)
grep -Eo "$RELOC_PATTERN" "$TMPDIR/nix-relocs.txt" | sort | uniq -c > "$TMPDIR/nix-reloc-types.txt"
grep -Eo "$RELOC_PATTERN" "$TMPDIR/cargo-relocs.txt" | sort | uniq -c > "$TMPDIR/cargo-reloc-types.txt"

if diff "$TMPDIR/nix-reloc-types.txt" "$TMPDIR/cargo-reloc-types.txt" > /dev/null; then
    echo "✅ Relocation types match"
else
    echo "❌ Relocation types differ"
    diff "$TMPDIR/nix-reloc-types.txt" "$TMPDIR/cargo-reloc-types.txt"
fi

echo ""
echo "=== Relocation Type Distribution ==="
cat "$TMPDIR/nix-reloc-types.txt"

echo ""
echo "=== Summary ==="
if [ "$NIX_SYMBOLS" -eq "$CARGO_SYMBOLS" ] && [ "$NIX_RELOCS" -eq "$CARGO_RELOCS" ] && diff "$TMPDIR/nix-reloc-types.txt" "$TMPDIR/cargo-reloc-types.txt" > /dev/null; then
    echo "✅ Builds are equivalent - both using maxperf profile"
else
    echo "❌ Builds differ"
    exit 1
fi
