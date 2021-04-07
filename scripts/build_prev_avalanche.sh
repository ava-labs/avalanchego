#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH
GOPATH="$(go env GOPATH)"
CURRENT_DIR="$(pwd)"

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
BUILD_DIR="$AVALANCHE_PATH/build" # Where binaries go
PLUGIN_DIR="$BUILD_DIR/plugins" # Where plugin binaries (namely coreth) go

PREV_AVALANCHEGO_VER="v1.3.1"
BINARY_PATH="$PLUGIN_DIR/avalanchego-$PREV_AVALANCHEGO_VER"

PREV_AVALANCHEGO_PATH="$GOPATH/pkg/mod/github.com/ava-labs/avalanchego@$PREV_AVALANCHEGO_VER"

if [[ $# -eq 2 ]]; then
    PREV_AVALANCHEGO_PATH=$1
    BINARY_PATH=$2
elif [[ $# -eq 0 ]]; then
    if [[ ! -d "$PREV_AVALANCHEGO_PATH" ]]; then
        go get "github.com/ava-labs/avalanchego@$PREV_AVALANCHEGO_VER"
    fi
else
    echo "Invalid arguments to build avalanchego. Requires either no arguments (default) or two arguments to specify avalanche previous version directory and location to add binary."
    exit 1
fi

# Build Avalanche previous version, which is run as a subprocess
echo "Building $BINARY_PATH..."
cd "$PREV_AVALANCHEGO_PATH"
GIT_COMMIT=$PREV_AVALANCHEGO_VER

# Build AVALANCHE
echo "Building Avalanche ${PREV_AVALANCHEGO_VER}..."
go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o "$BUILD_DIR/avalanchego-$PREV_AVALANCHEGO_VER" "$PREV_AVALANCHEGO_PATH/main/"*.go
cd "$CURRENT_DIR"
