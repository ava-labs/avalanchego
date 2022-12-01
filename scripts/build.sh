#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

print_usage() {
  printf "Usage: build [OPTIONS]

  Build avalanchego

  Options:

    -r  Build with race detector
"
}

race=''
while getopts 'r' flag; do
  case "${flag}" in
    r) race='-r' ;;
    *) print_usage
      exit 1 ;;
  esac
done

# Avalanchego root folder
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# Download dependencies
echo "Downloading dependencies..."
go mod download

build_args="$race"

# Build avalanchego
"$AVALANCHE_PATH"/scripts/build_avalanche.sh $build_args

# Build coreth
"$AVALANCHE_PATH"/scripts/build_coreth.sh $build_args

# Exit build successfully if the binaries are created
if [[ -f "$avalanchego_path" && -f "$evm_path" ]]; then
        echo "Build Successful"
        exit 0
else
        echo "Build failure" >&2
        exit 1
fi
