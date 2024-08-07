#!/usr/bin/env bash

set -euo pipefail

print_usage() {
  printf "Usage: build [OPTIONS]

  Build caminogo

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

# Caminogo root folder
CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$CAMINO_PATH"/scripts/constants.sh

# Download dependencies
echo "Downloading dependencies..."
(cd "$CAMINOGO_PATH" && go mod download)

build_args="$race"

# Build caminogo
"$CAMINO_PATH"/scripts/build_camino.sh $build_args

CAMINO_NETWORK_RUNNER_PATH="$CAMINOGO_PATH"/tools/camino-network-runner

if [ ! -f "$CAMINO_NETWORK_RUNNER_PATH"/.git ]; then
    echo "Initializing git submodules..."
    git --git-dir "$CAMINOGO_PATH"/.git submodule update --init --recursive
fi

# Build camino-network-runner
"$CAMINO_NETWORK_RUNNER_PATH"/scripts/build.sh

# Exit build successfully if the CaminoGo binary is created successfully
if [[ -f "$CAMINOGO_BIN_PATH" ]]; then
        echo "Build Successful"
        exit 0
else
        echo "Build failure" >&2
        exit 1
fi