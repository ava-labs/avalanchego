#!/usr/bin/env bash

set -euo pipefail

# Runs the fork-mode e2e suite (tests/fork) against a local tmpnet source
# network. Usage: ./scripts/tests.fork.sh [ginkgo args after --]

if ! [[ "$0" =~ scripts/tests.fork.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

AVALANCHEGO_PATH="$(realpath "${AVALANCHEGO_PATH:-./build/avalanchego}")"

# Sourcing constants.sh ensures that the necessary CGO flags are set to
# build the portable version of BLST.
source ./scripts/constants.sh

./bin/ginkgo -v ./tests/fork -- --avalanchego-path="${AVALANCHEGO_PATH}" "$@"
