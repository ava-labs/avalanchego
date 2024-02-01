#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "Building Genesis Generator..."

CAMINO_NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
source "$CAMINO_NODE_PATH"/scripts/constants.sh

# Download dependencies
if [ ! -f $CAMINO_NODE_PATH/dependencies/caminoethvm/.git ]; then
    echo "Initializing git submodules..."
    git --git-dir $CAMINO_NODE_PATH/.git submodule update --init --recursive
fi

# Load the constants
source "$CAMINO_NODE_PATH"/scripts/constants.sh

echo "Downloading dependencies..."
(cd $CAMINO_NODE_PATH && go mod download)

# Create tools directory
tools_dir=$build_dir/tools/
mkdir -p $tools_dir

target="$tools_dir/genesis-generator"
go build -ldflags="-s -w" -o "$target" "$CAMINO_NODE_PATH/tools/genesis/"*.go
