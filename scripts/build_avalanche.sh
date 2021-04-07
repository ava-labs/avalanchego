#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH
GOPATH="$(go env GOPATH)"

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
BUILD_DIR=$AVALANCHE_PATH/build # Where binaries go

GIT_COMMIT=$( git rev-list -1 HEAD )

# Build AVALANCHE
echo "Building Avalanche..."
go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o "$BUILD_DIR/avalanchego-1.3.2" "$AVALANCHE_PATH/app/"*.go

echo "Building previous version of Avalanche..."
# git checkout v1.3
#wget go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o "$BUILD_DIR/avalanchego-1.3.1" "$AVALANCHE_PATH/app/"*.go
# we'll also need coreth to follow the same procedure



echo "Building Avalanche BinaryManager..."
go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o "$BUILD_DIR/avalanchego" "$AVALANCHE_PATH/main/"*.go
