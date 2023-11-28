#!/usr/bin/env bash

set -euo pipefail

print_usage() {
  printf "Usage: build [OPTIONS]

  Build aoxc

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
AOXC_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AOXC_PATH"/scripts/constants.sh

# Download dependencies
echo "Downloading dependencies..."
go mod download

build_args="$race"

# Build aoxc
"$AOXC_PATH"/scripts/build_avalanche.sh $build_args

# Exit build successfully if the AvalancheGo binary is created successfully
if [[ -f "$aoxc_path" ]]; then
        echo "Build Successful"
        exit 0
else
        echo "Build failure" >&2
        exit 1
fi
