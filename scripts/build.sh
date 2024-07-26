#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Root directory
SUBNET_EVM_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# Load the versions
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

if [[ $# -eq 1 ]]; then
    PLUGIN_DIR=$1
elif [[ $# -eq 0 ]]; then
    PLUGIN_DIR="${DEFAULT_PLUGIN_DIR}"
else
    echo "Invalid arguments to build subnet-evm. Requires zero (default plugin dir) or one argument to specify the plugin dir."
    exit 1
fi

PLUGIN_PATH="${PLUGIN_DIR}/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"

BINARY_PATH="build/subnet-evm"

# Build Subnet EVM, which is run as a subprocess
echo "Building Subnet EVM @ GitCommit: $SUBNET_EVM_COMMIT at $BINARY_PATH"
go build -ldflags "-X github.com/ava-labs/subnet-evm/plugin/evm.GitCommit=$SUBNET_EVM_COMMIT $STATIC_LD_FLAGS" -o "./$BINARY_PATH" "plugin/"*.go

echo "Symlinking ${BINARY_PATH} to ${PLUGIN_PATH}"
mkdir -p "${PLUGIN_DIR}"
ln -sf "${PWD}/${BINARY_PATH}" "${PLUGIN_PATH}"
