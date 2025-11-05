#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Root directory
CORETH_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$CORETH_PATH"/scripts/versions.sh

# Load the constants
source "$CORETH_PATH"/scripts/constants.sh

if [[ $# -eq 1 ]]; then
    BINARY_PATH=$1
elif [[ $# -eq 0 ]]; then
    BINARY_PATH="$DEFAULT_PLUGIN_DIR/$DEFAULT_VM_ID"
else
    echo "Invalid arguments to build coreth. Requires zero (default binary path) or one argument to specify the binary path."
    exit 1
fi

# Check if CORETH_COMMIT is set, if not retrieve the last commit from the repo.
# This is used in the Dockerfile to allow a commit hash to be passed in without
# including the .git/ directory within the Docker image.
CORETH_COMMIT=${CORETH_COMMIT:-$(git rev-list -1 HEAD)}

# Build Coreth, which runs as a subprocess
echo "Building Coreth @ GitCommit: $CORETH_COMMIT at $BINARY_PATH"
go build -ldflags "-X github.com/ava-labs/avalanchego/graft/coreth/plugin/evm.GitCommit=$CORETH_COMMIT" -o "$BINARY_PATH" "plugin/"*.go
