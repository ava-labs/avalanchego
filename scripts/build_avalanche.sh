#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH
GOPATH="$(go env GOPATH)"

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
source $AVALANCHE_PATH/scripts/constants.sh


GIT_COMMIT=${AVALANCHEGO_COMMIT:-$( git rev-list -1 HEAD )}

# Build AVALANCHE
echo "Building AvalancheGo..."
go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o "$AVALANCHEGO_PROCESS_PATH" "$AVALANCHE_PATH/app/"*.go

echo "Building AvalancheGo binary manager..."
go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o "$BINARY_MANAGER_PATH" "$AVALANCHE_PATH/main/"*.go
