#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# Download dependencies
echo "Downloading dependencies..."
go mod download

# Set GOPATH
GOPATH="$(go env GOPATH)"

GECKO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
BUILD_DIR=$GECKO_PATH/build # Where binaries go
PLUGIN_DIR="$BUILD_DIR/plugins" # Where plugin binaries (namely coreth) go

CORETH_VER="0.1.0" # Should match coreth version in go.mod
CORETH_PATH="$GOPATH/pkg/mod/github.com/ava-labs/coreth@v$CORETH_VER"

# Build Gecko
echo "Building Gecko..."
go build -o "$BUILD_DIR/ava" "$GECKO_PATH/main/"*.go

# Build Coreth, which is run as a subprocess by Gecko
echo "Building Coreth..."
go build -o "$PLUGIN_DIR/evm" "$CORETH_PATH/plugin/"*.go

if [[ -f "$BUILD_DIR/ava" && -f "$PLUGIN_DIR/evm" ]]; then
        echo "Build Successful" 
else
        echo "Build failure" 
fi
