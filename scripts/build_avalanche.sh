#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

git_commit=${AVALANCHEGO_COMMIT:-$( git rev-list -1 HEAD )}

# Build AVALANCHE
echo "Building Avalanche @ ${git_commit} ..."
go build -ldflags "-X main.GitCommit=$git_commit" -o "$build_dir/avalanchego" "$AVALANCHE_PATH/main/"*.go
