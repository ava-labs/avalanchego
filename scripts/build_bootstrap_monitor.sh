#!/usr/bin/env bash

set -euo pipefail

# Avalanchego root folder
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh
source "$AVALANCHE_PATH"/scripts/vcs.sh

echo "Building bootstrap-monitor..."
go build -ldflags\
   "-X github.com/ava-labs/avalanchego/version.GitCommit=$vcs_commit $static_ld_flags"\
   -o "$AVALANCHE_PATH/build/bootstrap-monitor"\
   "$AVALANCHE_PATH/tests/fixture/bootstrapmonitor/cmd/"*.go
