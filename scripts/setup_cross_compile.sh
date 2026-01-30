#!/usr/bin/env bash

# Configures cross-compilation for Docker builds.
# Usage: ./setup_cross_compile.sh "$TARGETPLATFORM" "$BUILDPLATFORM"

set -euo pipefail

TARGETPLATFORM="${1:-}"
BUILDPLATFORM="${2:-}"

if [ -z "$TARGETPLATFORM" ] || [ -z "$BUILDPLATFORM" ]; then
  echo "Error: TARGETPLATFORM and BUILDPLATFORM arguments required"
  echo "Usage: $0 <TARGETPLATFORM> <BUILDPLATFORM>"
  exit 1
fi

if [ "$TARGETPLATFORM" = "linux/arm64" ] && [ "$BUILDPLATFORM" != "linux/arm64" ]; then
  echo "Cross-compiling for linux/arm64 on $BUILDPLATFORM"
  apt-get update && apt-get install -y gcc-aarch64-linux-gnu
  echo "export CC=aarch64-linux-gnu-gcc" > ./build_env.sh
elif [ "$TARGETPLATFORM" = "linux/amd64" ] && [ "$BUILDPLATFORM" != "linux/amd64" ]; then
  echo "Cross-compiling for linux/amd64 on $BUILDPLATFORM"
  apt-get update && apt-get install -y gcc-x86-64-linux-gnu
  echo "export CC=x86_64-linux-gnu-gcc" > ./build_env.sh
else
  echo "Native compilation for $TARGETPLATFORM"
  echo "export CC=gcc" > ./build_env.sh
fi

echo "Cross-compilation setup complete"
