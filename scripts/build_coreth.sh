#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH
GOPATH="$(go env GOPATH)"

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
BUILD_DIR="$AVALANCHE_PATH/build" # Where binaries go
PLUGIN_DIR="$BUILD_DIR/plugins" # Where plugin binaries (namely coreth) go
BINARY_PATH="$PLUGIN_DIR/evm"

CORETH_VER="0.3.4" # Should match coreth version in go.mod
CORETH_PATH="$GOPATH/pkg/mod/github.com/ava-labs/coreth@v$CORETH_VER"

if [[ $# -eq 2 ]]; then
    CORETH_PATH=$1
    BINARY_PATH=$2
elif [[ $# -ne 0 ]]; then
    echo "Invalid arguments to build coreth. Requires either no arguments (default) or two arguments to specify coreth directory and location to add binary."
    exit 1
fi

# Build Coreth, which is run as a subprocess
echo "Building Coreth..."
go build -o "$BINARY_PATH" "$CORETH_PATH/plugin/"*.go
