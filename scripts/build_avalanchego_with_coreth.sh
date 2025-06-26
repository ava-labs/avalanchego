#!/bin/bash

# This script builds a new AvalancheGo binary with the Coreth dependency pointing to the local Coreth path
# Usage: ./build_avalanchego_with_coreth.sh with optional AVALANCHEGO_VERSION and AVALANCHEGO_CLONE_PATH environment variables

set -euo pipefail

# Coreth root directory
CORETH_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Allow configuring the clone path to point to an existing clone
AVALANCHEGO_CLONE_PATH="${AVALANCHEGO_CLONE_PATH:-avalanchego}"

# Load the version
source "$CORETH_PATH"/scripts/versions.sh

# Always return to the coreth path on exit
function cleanup {
  cd "${CORETH_PATH}"
}
trap cleanup EXIT

echo "checking out target AvalancheGo version ${AVALANCHE_VERSION}"
if [[ -d "${AVALANCHEGO_CLONE_PATH}" ]]; then
  echo "updating existing clone"
  cd "${AVALANCHEGO_CLONE_PATH}"
  git fetch
else
  echo "creating new clone"
  git clone https://github.com/ava-labs/avalanchego.git "${AVALANCHEGO_CLONE_PATH}"
  cd "${AVALANCHEGO_CLONE_PATH}"
fi
# Branch will be reset to $AVALANCHE_VERSION if it already exists
git checkout -B "test-${AVALANCHE_VERSION}" "${AVALANCHE_VERSION}"

echo "updating coreth dependency to point to ${CORETH_PATH}"
go mod edit -replace "github.com/ava-labs/coreth=${CORETH_PATH}"
go mod tidy

echo "building avalanchego"
./scripts/build.sh
