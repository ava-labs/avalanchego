#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# check if there's args defining different coreth source and build paths
if [[ $# -eq 2 ]]; then
    coreth_path=$1
    evm_path=$2
elif [[ $# -eq 0 ]]; then
    if [[ ! -d "$coreth_path" ]]; then
        go get "github.com/ava-labs/coreth@$coreth_version"
    fi
else
    echo "Invalid arguments to build coreth. Requires either no arguments (default) or two arguments to specify coreth directory and location to add binary."
    exit 1
fi

# Build Coreth
echo "Building Coreth @ ${coreth_version} ..."
cd "$coreth_path"
go build -ldflags "-X github.com/ava-labs/coreth/plugin/evm.Version=$coreth_version $static_ld_flags" -o "$evm_path" "plugin/"*.go
cd "$AVALANCHE_PATH"

# Building coreth + using go get can mess with the go.mod file.
go mod tidy
