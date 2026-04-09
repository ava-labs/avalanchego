#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${AVALANCHE_PATH}"

NODE_COUNT="${NODE_COUNT:-5}"

echo "Building and starting ${NODE_COUNT}-node local network..."
./scripts/build.sh
./scripts/run_tmpnetctl.sh start-network --node-count="${NODE_COUNT}" --avalanchego-path=./build/avalanchego
