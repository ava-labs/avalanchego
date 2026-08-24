#!/usr/bin/env bash
# One-shot ERC-20 throughput benchmark: builds the node and the subnet-evm
# plugin (with the bencherc20 precompile), boots a local devnet, runs all
# three levels, prints the comparison. Everything else is in main.go.
set -euo pipefail
cd "$(dirname "$0")/../.."

mkdir -p build/plugins
echo "building avalanchego..."
go build -o build/avalanchego ./main
echo "building subnet-evm plugin..."
go build -o build/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy ./graft/subnet-evm/plugin

exec go run ./tests/erc20bench "$@"
