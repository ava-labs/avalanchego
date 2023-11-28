#!/usr/bin/env bash

set -euo pipefail

# Avalanchego root folder
AOXC_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AOXC_PATH"/scripts/constants.sh

echo "Building testnetctl..."
go build -ldflags\
   "-X github.com/aoxc/aoxc/version.GitCommit=$git_commit $static_ld_flags"\
   -o "$AOXC_PATH/build/testnetctl"\
   "$AOXC_PATH/tests/fixture/testnet/cmd/"*.go
