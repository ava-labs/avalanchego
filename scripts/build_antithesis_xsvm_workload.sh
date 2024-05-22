#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

echo "Building Workload..."
go build -o "$AVALANCHE_PATH/build/antithesis-xsvm-workload" "$AVALANCHE_PATH/tests/antithesis/xsvm/"*.go
