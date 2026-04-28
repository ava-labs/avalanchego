#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
AVALANCHE_PATH=$(cd "${SCRIPT_DIR}/../.." && pwd)

cd "${AVALANCHE_PATH}"

./scripts/simplex/clean.sh
./scripts/build.sh
./scripts/run_tmpnetctl.sh start-network --node-count=5 --avalanchego-path=./build/avalanchego
./scripts/simplex/create_l1.sh
./scripts/simplex/fund_nodes.sh
