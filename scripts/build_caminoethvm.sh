#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$CAMINO_PATH"/scripts/versions.sh

# Load the constants
source "$CAMINO_PATH"/scripts/constants.sh

# check if there's args defining different caminoethvm source and build paths
if [[ $# -eq 2 ]]; then
    caminoethvm_path=$1
    evm_path=$2
elif [[ $# -eq 0 ]]; then
    if [[ ! -d "$caminoethvm_path" ]]; then
        go get "github.com/chain4travel/caminoethvm@$caminoethvm_version"
    fi
else
    echo "Invalid arguments to build caminoethvm. Requires either no arguments (default) or two arguments to specify caminoethvm directory and location to add binary."
    exit 1
fi

# Build Coreth
echo "Building Coreth @ ${caminoethvm_version} ..."
cd "$caminoethvm_path"
go build -ldflags "-X github.com/chain4travel/caminoethvm/plugin/evm.Version=$caminoethvm_version $static_ld_flags" -o "$evm_path" "plugin/"*.go
cd "$CAMINO_PATH"

# Building caminoethvm + using go get can mess with the go.mod file.
go mod tidy
