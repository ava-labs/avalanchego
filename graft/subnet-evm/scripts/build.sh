#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SUBNET_EVM_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)
REPO_ROOT="$SUBNET_EVM_PATH/../.."

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

if [[ $# -eq 1 ]]; then
    BINARY_PATH=$1
elif [[ $# -eq 0 ]]; then
    BINARY_PATH="${AVALANCHEGO_BUILD_PATH:-$REPO_ROOT/build}/subnet-evm"
else
    echo "Invalid arguments to build subnet-evm. Requires zero (default binary path) or one argument to specify the binary path."
    exit 1
fi

# Build Subnet EVM, which is run as a subprocess
echo "Building Subnet EVM @ GitCommit: $SUBNET_EVM_COMMIT at $BINARY_PATH"
(cd "$SUBNET_EVM_PATH" && go build -ldflags "-X github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm.GitCommit=$SUBNET_EVM_COMMIT $STATIC_LD_FLAGS" -o "$BINARY_PATH" ./plugin)

# Symlink to both global and local plugin directories to simplify
# usage for testing. The local directory should be preferred but the
# global directory remains supported for backwards compatibility.
LOCAL_PLUGIN_PATH="$REPO_ROOT/build/plugins"
GLOBAL_PLUGIN_PATH="${HOME}/.avalanchego/plugins"
for plugin_dir in "${GLOBAL_PLUGIN_PATH}" "${LOCAL_PLUGIN_PATH}"; do
    PLUGIN_PATH="${plugin_dir}/${DEFAULT_VM_ID}"
    echo "Symlinking ${BINARY_PATH} to ${PLUGIN_PATH}"
    mkdir -p "${plugin_dir}"
    ln -sf "$(cd "$(dirname "$BINARY_PATH")" && pwd)/$(basename "$BINARY_PATH")" "${PLUGIN_PATH}"
done