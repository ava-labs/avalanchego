#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH
GOPATH="$(go env GOPATH)"
CURRENT_DIR="$(pwd)"
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
BUILD_DIR="$AVALANCHE_PATH/build" # Where binaries go
PLUGIN_DIR="$BUILD_DIR/plugins" # Where plugin binaries (namely coreth) go
PREV_AVALANCHEGO_VER="v1.3.1"

rm -rf tmp
mkdir tmp
echo "fetching pre-db upgrade version..."
git clone --quiet https://github.com/ava-labs/avalanchego tmp
cd tmp
git checkout --quiet $PREV_AVALANCHEGO_VER

# Run the pre-db upgrade version's build script
echo "building pre-db upgrade AvalancheGo..."
"$CURRENT_DIR/tmp/scripts/build.sh" > /dev/null
# Copy the binaries to where we expect them
mkdir -p $BUILD_DIR/avalanchego-$PREV_AVALANCHEGO_VER/plugins
mv $CURRENT_DIR/tmp/build/avalanchego "$BUILD_DIR/avalanchego-$PREV_AVALANCHEGO_VER/avalanchego"
mv  $CURRENT_DIR/tmp/build/plugins/* "$BUILD_DIR/avalanchego-$PREV_AVALANCHEGO_VER/plugins"
rm -rf tmp
