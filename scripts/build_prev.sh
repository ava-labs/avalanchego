#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CURRENT_DIR="$(pwd)"

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
source $AVALANCHE_PATH/scripts/constants.sh

rm -rf tmp
mkdir tmp
echo "Fetching AvalancheGo ${PREV_AVALANCHEGO_VER}..."
git clone --quiet https://github.com/ava-labs/avalanchego-internal tmp
cd tmp
git checkout --quiet $PREV_AVALANCHEGO_VER

# Run the pre-db upgrade version's build script
echo "Building AvalancheGo ${PREV_AVALANCHEGO_VER}..."
"$CURRENT_DIR/tmp/scripts/build.sh" > /dev/null
# Copy the binaries to where we expect them
mkdir -p $PREV_PLUGIN_DIR
mv $CURRENT_DIR/tmp/build/avalanchego "$PREV_AVALANCHEGO_INNER_PATH"
mv  $CURRENT_DIR/tmp/build/plugins/* "$PREV_PLUGIN_DIR"
rm -rf tmp
