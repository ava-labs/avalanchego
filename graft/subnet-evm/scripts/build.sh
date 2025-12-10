#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Root directory
SUBNET_EVM_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

if [[ $# -eq 1 ]]; then
    BINARY_PATH=$1
elif [[ $# -eq 0 ]]; then
    BINARY_PATH="$DEFAULT_PLUGIN_DIR/$DEFAULT_VM_ID"
else
    echo "Invalid arguments to build subnet-evm. Requires zero (default binary path) or one argument to specify the binary path."
    exit 1
fi

# Build Subnet EVM, which is run as a subprocess
echo "Building Subnet EVM @ GitCommit: $SUBNET_EVM_COMMIT at $BINARY_PATH"
go build -ldflags "-X github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm.GitCommit=$SUBNET_EVM_COMMIT $STATIC_LD_FLAGS" -o "$BINARY_PATH" "plugin/"*.go
