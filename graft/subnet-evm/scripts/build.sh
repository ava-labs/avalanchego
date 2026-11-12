#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SUBNET_EVM_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# shellcheck disable=SC1091
source "$SUBNET_EVM_PATH"/../../scripts/lib_build_plugin.sh
# shellcheck disable=SC1091
source "$SUBNET_EVM_PATH"/scripts/vm_constants.sh

if [[ $# -eq 1 ]]; then
    BINARY_PATH=$1
elif [[ $# -eq 0 ]]; then
    BINARY_PATH="${PLUGIN_BUILD_DIR}/subnet-evm"
else
    echo "Invalid arguments to build subnet-evm. Requires zero (default binary path) or one argument to specify the binary path."
    exit 1
fi

# Build Subnet EVM, which is run as a subprocess
cd "$SUBNET_EVM_PATH"
build_plugin "$BINARY_PATH" "$DEFAULT_VM_ID" ./plugin/
