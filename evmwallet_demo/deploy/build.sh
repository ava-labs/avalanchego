#!/bin/bash
# Builds the linux/amd64 binaries launch.sh ships to the box.
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT=${BIN:-/tmp/evmwallet-demo-bin}
mkdir -p "$OUT"
GOOS=linux GOARCH=amd64 go build -o "$OUT/avalanchego" ./main
GOOS=linux GOARCH=amd64 go build -o "$OUT/bootstrap" ./evmwallet_demo
echo "binaries in $OUT"
