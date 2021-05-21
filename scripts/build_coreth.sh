#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH
CURRENT_DIR="$(pwd)"

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
source $AVALANCHE_PATH/scripts/constants.sh

if [[ $# -eq 2 ]]; then
    CORETH_PATH=$1
    EVM_PATH=$2
elif [[ $# -eq 0 ]]; then
    if [[ ! -d "$CORETH_PATH" ]]; then
        go get "github.com/ava-labs/coreth@$CORETH_VER"
    fi
else
    echo "Invalid arguments to build coreth. Requires either no arguments (default) or two arguments to specify coreth directory and location to add binary."
    exit 1
fi

# Build Coreth, which is run as a subprocess
echo "Building Coreth..."
cd "$CORETH_PATH"
go build -o "$EVM_PATH" "plugin/"*.go
cd "$CURRENT_DIR"

# Building coreth + using go get can mess with the go.mod file.
go mod tidy
