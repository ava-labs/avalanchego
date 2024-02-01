#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "Building..."

# Changes to the minimum golang version must also be replicated in
# scripts/ansible/roles/golang_base/defaults/main.yml (here)
# scripts/build_camino.sh (here)
# scripts/local.Dockerfile
# Dockerfile
# README.md
# go.mod
go_version_minimum="1.19"

go_version() {
    go version | sed -nE -e 's/[^0-9.]+([0-9.]+).+/\1/p'
}

version_lt() {
    # Return true if $1 is a lower version than than $2,
    local ver1=$1
    local ver2=$2
    # Reverse sort the versions, if the 1st item != ver1 then ver1 < ver2
    if  [[ $(echo -e -n "$ver1\n$ver2\n" | sort -rV | head -n1) != "$ver1" ]]; then
        return 0
    else
        return 1
    fi
}

if version_lt "$(go_version)" "$go_version_minimum"; then
    echo "Camino-Node requires Go >= $go_version_minimum, Go $(go_version) found." >&2
    exit 1
fi

# Camino-Node root folder
CAMINO_NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$CAMINO_NODE_PATH"/scripts/constants.sh

LDFLAGS="-X github.com/chain4travel/camino-node/version.GitCommit=$git_commit"
LDFLAGS="$LDFLAGS -X github.com/chain4travel/camino-node/version.GitVersion=$git_tag"
LDFLAGS="$LDFLAGS -X github.com/ava-labs/avalanchego/version.GitCommit=$caminogo_commit"
LDFLAGS="$LDFLAGS -X github.com/ava-labs/avalanchego/version.GitVersion=$caminogo_tag"
LDFLAGS="$LDFLAGS -X github.com/ava-labs/coreth/plugin/evm.GitCommit=$caminoethvm_commit"
LDFLAGS="$LDFLAGS -X github.com/ava-labs/coreth/plugin/evm.Version=$caminoethvm_tag"
LDFLAGS="$LDFLAGS $static_ld_flags"

go build -ldflags "$LDFLAGS" -o "$camino_node_path" "$CAMINO_NODE_PATH/main/"*.go

# Make plugin folder
mkdir -p $plugin_dir

# Exit build successfully if the binaries are created
if [[ -f "$camino_node_path" ]]; then
    echo "Build Successful"
    exit 0
else
    echo "Build failure" >&2
    exit 1
fi
