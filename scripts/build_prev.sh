#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CURRENT_DIR="$(pwd)"

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
source $AVALANCHE_PATH/scripts/constants.sh

rm -rf tmp
echo "Fetching AvalancheGo ${PREUPGRADE_AVALANCHEGO_VER}..."
git clone -b $PREUPGRADE_AVALANCHEGO_VER --single-branch --quiet https://github.com/ava-labs/avalanchego-internal tmp
cd tmp

# Run the pre-db upgrade version's build script
echo "Building AvalancheGo ${PREUPGRADE_AVALANCHEGO_VER}..."
"$CURRENT_DIR/tmp/scripts/build.sh" > /dev/null
# Copy the binaries to where we expect them
mkdir -p $PREV_PLUGIN_DIR
mv $CURRENT_DIR/tmp/build/avalanchego "$PREV_AVALANCHEGO_PROCESS_PATH"
mv  $CURRENT_DIR/tmp/build/plugins/* "$PREV_PLUGIN_DIR"
cd -
rm -rf tmp
