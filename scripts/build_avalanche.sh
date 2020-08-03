#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH
GOPATH="$(go env GOPATH)"

GECKO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
BUILD_DIR=$GECKO_PATH/build # Where binaries go

# Build Gecko
echo "Building Gecko..."
go build -o "$BUILD_DIR/ava" "$GECKO_PATH/main/"*.go
