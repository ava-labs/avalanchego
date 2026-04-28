#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${AVALANCHE_PATH}"

echo "Stopping local network..."
./scripts/run_tmpnetctl.sh stop-network
