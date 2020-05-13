#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# Fetch Gecko dependencies, including salticidae-go and coreth
echo "Fetching dependencies..."
go mod download

GECKO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
source ${GECKO_PATH}/scripts/env.sh 

# Make sure specified versions of salticidae and coreth exist
if [ ! -d $CORETH_PATH ]; then
    echo "couldn't find coreth version ${CORETH_VER} at ${CORETH_PATH}"
    echo "build failed"
    exit 1
elif [ ! -d $SALTICIDAE_GO_PATH ]; then
    echo "couldn't find salticidae-go version ${SALTICIDAE_GO_VER} at ${SALTICIDAE_GO_PATH}"
    echo "build failed"
    exit 1
fi

# Build salticidae
echo "Building salticidae-go..."
# Exports CGO_CFLAGS and CGO_LDFLAGS so go compiler can find salticidae
chmod +w $SALTICIDAE_GO_PATH
source $SALTICIDAE_GO_PATH/scripts/build.sh 

# Build the binaries
echo "Building Gecko binary..."
go build -o "$BUILD_DIR/ava" "$GECKO_PATH/main/"*.go

echo "Building throughput test binary..."
go build -o "$BUILD_DIR/xputtest" "$GECKO_PATH/xputtest/"*.go

echo "Building EVM plugin binary..."
go build -o "$BUILD_DIR/plugins/evm" "$CORETH_PATH/plugin/"*.go

if [[ -f "$BUILD_DIR/ava" && -f "$BUILD_DIR/xputtest" && -f "$BUILD_DIR/plugins/evm" ]]; then
        echo "Build successful" 
else
        echo "Build failed" 
fi