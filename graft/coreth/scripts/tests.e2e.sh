#!/usr/bin/env bash

set -euo pipefail

# Run AvalancheGo e2e tests from the target version against the current state of coreth.

# e.g.,
# ./scripts/tests.e2e.sh
# AVALANCHE_VERSION=v1.10.x ./scripts/tests.e2e.sh
# ./scripts/tests.e2e.sh --start-monitors          # All arguments are supplied to ginkgo
if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Coreth root directory
CORETH_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Allow configuring the clone path to point to an existing clone
AVALANCHEGO_CLONE_PATH="${AVALANCHEGO_CLONE_PATH:-avalanchego}"

# Always return to the coreth path on exit
function cleanup {
  cd "${CORETH_PATH}"
}

trap cleanup EXIT

cd "${AVALANCHEGO_CLONE_PATH}"

echo "running AvalancheGo e2e tests"
./scripts/run_task.sh test-e2e-ci -- --ginkgo.label-filter='c || uses-c' "${@}"
