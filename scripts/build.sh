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

# Create the plugin directory required by AvalancheGo
mkdir -p $plugin_dir

# Build avalanchego
"$AVALANCHE_PATH"/scripts/build_avalanche.sh

# Exit build successfully if the AvalancheGo binary is created successfully
if [[ -f "$avalanchego_path" ]]; then
        echo "Build Successful"
        exit 0
else
        echo "Build failure" >&2
        exit 1
fi
