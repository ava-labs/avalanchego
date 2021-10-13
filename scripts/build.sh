#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanchego root folder
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# Download dependencies
echo "Downloading dependencies..."
go mod download

# Build avalanchego
"$AVALANCHE_PATH"/scripts/build_avalanche.sh

# Build coreth
"$AVALANCHE_PATH"/scripts/build_coreth.sh

# Exit build successfully if the binaries are created
if [[ -f "$avalanchego_path" && -f "$evm_path" ]]; then
        echo "Build Successful"
        exit 0
else
        echo "Build failure" >&2
        exit 1
fi
