#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH=$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
cd "${AVALANCHE_PATH}"

# Build the binary before execution to ensure it is always up-to-date. Faster than `go run`.
./scripts/build.sh
./build/avalanchego "${@}"
