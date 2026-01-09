#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

echo "Building Subnet-EVM Workload..."
go build -o "$AVALANCHE_PATH/build/antithesis-subnet-evm-workload" "$AVALANCHE_PATH/tests/antithesis/subnet-evm/"*.go
