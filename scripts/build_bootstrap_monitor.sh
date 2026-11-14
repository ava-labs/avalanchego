#!/usr/bin/env bash

set -euo pipefail

# Avalanchego root folder
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh
source "$AVALANCHE_PATH"/scripts/git_commit.sh

echo "Building bootstrap-monitor..."
go build -ldflags "${go_ldflags}"\
   -o "$AVALANCHE_PATH/build/bootstrap-monitor"\
   "$AVALANCHE_PATH/tests/fixture/bootstrapmonitor/cmd/"*.go
