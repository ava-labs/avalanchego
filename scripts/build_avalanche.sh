#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Changes to the minimum golang version must also be replicated in
# scripts/ansible/roles/golang_based/defaults/main.yml (here)
# scripts/build_avalanche.sh (here)
# scripts/local.Dockerfile
# Dockerfile
# README.md
# go.mod
GO_VERSION_MINIMUM="1.15.5"

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

if version_lt "$(go_version)" "$GO_VERSION_MINIMUM"; then
    echo "AvalancheGo requires Go >= $GO_VERSION_MINIMUM, Go $(go_version) found." >&2
    exit 1
fi

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# Build AVALANCHE
echo "Building AvalancheGo..."
go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o "$AVALANCHEGO_PROCESS_PATH" "$AVALANCHE_PATH/app/"*.go

echo "Building AvalancheGo binary manager..."
go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o "$BINARY_MANAGER_PATH" "$AVALANCHE_PATH/main/"*.go
git_commit=${AVALANCHEGO_COMMIT:-$( git rev-list -1 HEAD )}
