#!/bin/bash

set -e

# Build Firewood from source for a specific commit/branch
# Usage: ./scripts/build_firewood.sh <commit_or_branch> [output_dir]
#
# Prerequisites: Rust toolchain must be installed and available in PATH
# The calling workflow/environment is responsible for setting up Rust
#
# This script:
# 1. Clones Firewood repository
# 2. Builds Firewood with required features
# 3. Sets up FFI directory structure for Go integration

# Source constants to get project paths
source "$(dirname "${BASH_SOURCE[0]}")/constants.sh"

# Parse arguments
FIREWOOD_VERSION=${1:-"main"}
OUTPUT_DIR=${2:-"./ffi"}

echo "Building Firewood $FIREWOOD_VERSION from source"

# Detect platform for library placement
OS=$(uname -s)
ARCH=$(uname -m)

case "$OS-$ARCH" in
    Linux-x86_64) PLATFORM="x86_64-unknown-linux-gnu" ;;
    Linux-aarch64|Linux-arm64) PLATFORM="aarch64-unknown-linux-gnu" ;;
    Darwin-x86_64) PLATFORM="x86_64-apple-darwin" ;;
    Darwin-arm64) PLATFORM="aarch64-apple-darwin" ;;
    *) echo "Unsupported platform: $OS-$ARCH"; exit 1 ;;
esac

# Create temporary directory for build
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

cd "$TEMP_DIR"

# Clone and checkout Firewood
git clone https://github.com/ava-labs/firewood.git
cd firewood
git checkout "$FIREWOOD_VERSION"

# Build Firewood with required features
cargo build --profile maxperf --features ethhash,logger

# Verify the build produced the required library
BUILT_LIB=$(find target -name "libfirewood_ffi.a" | head -1)
if [ -z "$BUILT_LIB" ]; then
    echo "Error: libfirewood_ffi.a not found after build"
    exit 1
fi

# Go back to original directory and set up output
cd "$AVALANCHE_PATH"

# Create FFI directory structure
rm -rf "$OUTPUT_DIR"
cp -r "$TEMP_DIR/firewood/ffi" "$OUTPUT_DIR"

# Create platform-specific library directory and copy library
PLATFORM_LIB_DIR="$OUTPUT_DIR/libs/$PLATFORM"
mkdir -p "$PLATFORM_LIB_DIR"
cp "$TEMP_DIR/firewood/$BUILT_LIB" "$PLATFORM_LIB_DIR/"

# Verify final setup
if [ ! -f "$PLATFORM_LIB_DIR/libfirewood_ffi.a" ]; then
    echo "Error: Failed to copy library to platform directory"
    exit 1
fi

ABS_OUTPUT_DIR=$(cd "$OUTPUT_DIR" && pwd)
echo "Firewood build complete: $ABS_OUTPUT_DIR"
