#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Changes to the minimum golang version must also be replicated in
# scripts/ansible/roles/golang_base/defaults/main.yml (here)
# scripts/build_camino.sh (here)
# scripts/local.Dockerfile
# Dockerfile
# README.md
# go.mod
go_version_minimum="1.17.9"

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
    echo "CaminoGo requires Go >= $go_version_minimum, Go $(go_version) found." >&2
    exit 1
fi

# Caminogogo root folder
CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the versions
source "$CAMINO_PATH"/scripts/versions.sh
# Load the constants
source "$CAMINO_PATH"/scripts/constants.sh

# Build with rocksdb allowed only if the environment variable ROCKSDBALLOWED is set
if [ -z ${ROCKSDBALLOWED+x} ]; then
    echo "Building CaminoGo..."
    go build -ldflags "-X github.com/chain4travel/caminogo/version.GitCommit=$git_commit $static_ld_flags" -o "$caminogo_path" "$CAMINO_PATH/main/"*.go
else
    echo "Building AvalancheGo with rocksdb enabled..."
    go build -tags rocksdballowed -ldflags "-X github.com/chain4travel/caminogo/version.GitCommit=$git_commit $static_ld_flags" -o "$caminogo_path" "$CAMINO_PATH/main/"*.go
fi
