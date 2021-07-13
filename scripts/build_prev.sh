#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CURRENT_DIR="$(pwd)"

# Avalanchego root folder
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

rm -rf tmp
echo "Fetching AvalancheGo ${prev_avalanchego_version}..."
git clone -b $prev_avalanchego_version --single-branch --quiet https://github.com/ava-labs/avalanchego tmp
cd tmp

# Run the pre-db upgrade version's build script
echo "Building AvalancheGo ${prev_avalanchego_version}..."
"$CURRENT_DIR/tmp/scripts/build.sh" > /dev/null
# Copy the binaries to where we expect them
mkdir -p $prev_plugin_dir
mv $CURRENT_DIR/tmp/build/avalanchego "$prev_avalanchego_process_path"
mv  $CURRENT_DIR/tmp/build/plugins/* "$prev_plugin_dir"
cd -
rm -rf tmp
