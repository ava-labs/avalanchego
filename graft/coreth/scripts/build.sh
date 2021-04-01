#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH
GOPATH="$(go env GOPATH)"

# Set default binary location
BINARY_PATH="$GOPATH/src/github.com/ava-labs/avalanchego/build/plugins/evm"

if [[ $# -eq 1 ]]; then
    BINARY_PATH=$1
elif [[ $# -ne 0 ]]; then
    echo "Invalid arguments to build coreth. Requires either no arguments (default) or one arguments to specify binary location."
    exit 1
fi

# Check if CORETH_COMMIT is set, if not retrieve the last commit from the repo.
# This is used in the Dockerfile to allow a commit hash to be passed in without
# including the .git/ directory within the Docker image.
CORETH_COMMIT=${CORETH_COMMIT:-$( git rev-list -1 HEAD )}

# Build Coreth, which is run as a subprocess
echo "Building Coreth from GitCommit: $CORETH_COMMIT"
go build -ldflags "-X github.com/ava-labs/coreth/plugin/evm.GitCommit=$CORETH_COMMIT" -o "$BINARY_PATH" "plugin/"*.go
