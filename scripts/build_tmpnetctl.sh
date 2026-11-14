#!/usr/bin/env bash

set -euo pipefail

# Avalanchego root folder
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh
source "$AVALANCHE_PATH"/scripts/git_commit.sh

echo "Building tmpnetctl..."
go build -ldflags "${go_ldflags}"\
   -o "$AVALANCHE_PATH/build/tmpnetctl"\
   "$AVALANCHE_PATH/tests/fixture/tmpnet/tmpnetctl/"*.go
