#!/usr/bin/env bash

# First argument is the time, in seconds, to run each fuzz test for.
# If not provided, defaults to 1 second.
#
# Second argument is the directory to run fuzz tests in.
# If not provided, defaults to the current directory.

set -euo pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

echo "Building Workload..."
go build -o "$AVALANCHE_PATH/build/workload" "$AVALANCHE_PATH/tests/antithesis/"*.go