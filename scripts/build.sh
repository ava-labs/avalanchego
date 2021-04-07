#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Download dependencies
echo "Downloading dependencies..."
go mod download

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
source $AVALANCHE_PATH/scripts/constants.sh

"$AVALANCHE_PATH/scripts/build_avalanche.sh"
"$AVALANCHE_PATH/scripts/build_coreth.sh"

if [[ ! -d "$BUILD_DIR/avalanchego-$PREV_AVALANCHEGO_VER" ]]; then
        "$AVALANCHE_PATH/scripts/build_prev.sh"
fi


if [[ -f "$AVALANCHEGO_INNER_PATH" && -f "$EVM_PATH" ]]; then
        echo "Build Successful"
        exit 0
else
        echo "Build failure" 
        exit 1
fi
