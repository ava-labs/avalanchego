#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${AVALANCHE_PATH}"

NODE_COUNT="${NODE_COUNT:-5}"

PLUGIN_NAME="srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"

echo "Building and starting ${NODE_COUNT}-node local network..."
./scripts/build.sh

echo "Building subnet-evm plugin..."
mkdir -p "${HOME}/.avalanchego/plugins"
go build -o "${HOME}/.avalanchego/plugins/${PLUGIN_NAME}" ./graft/subnet-evm/plugin/

./scripts/run_tmpnetctl.sh start-network --node-count="${NODE_COUNT}" --avalanchego-path=./build/avalanchego
