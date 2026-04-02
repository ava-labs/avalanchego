#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
source "$AVALANCHE_PATH"/scripts/constants.sh
source "$AVALANCHE_PATH"/scripts/git_commit.sh

echo "Building gh-pending-review..."
go build -ldflags \
   "-X github.com/ava-labs/avalanchego/version.GitCommit=$git_commit $static_ld_flags" \
   -o "$AVALANCHE_PATH/build/gh-pending-review" \
   "$AVALANCHE_PATH/tools/pendingreview/cmd"
