#!/usr/bin/env bash

set -euo pipefail

# This script verifies that a network can be reused across test runs.

# e.g.,
# ./scripts/build.sh
# ./scripts/tests.e2e.sh --ginkgo.label-filter=x                        # All arguments are supplied to ginkgo
# AVALANCHEGO_PATH=./build/avalanchego ./scripts/tests.e2e.existing.sh  # Customization of avalanchego path
if ! [[ "$0" =~ scripts/tests.e2e.existing.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Provide visual separation between testing and setup/teardown
function print_separator {
  printf '%*s\n' "${COLUMNS:-80}" '' | tr ' ' ─
}

# Ensure network cleanup on teardown
function cleanup {
  print_separator
  echo "cleaning up reusable network"
  ginkgo -v ./tests/e2e/e2e.test -- --stop-network
}
trap cleanup EXIT

print_separator
echo "starting initial test run that should create the reusable network"
./scripts/tests.e2e.sh --reuse-network --ginkgo.focus-file=xsvm.go "${@}"

print_separator
echo "determining the network path of the reusable network created by the first test run"
SYMLINK_PATH="${HOME}/.tmpnet/networks/latest_avalanchego-e2e"
INITIAL_NETWORK_DIR="$(realpath "${SYMLINK_PATH}")"

print_separator
echo "starting second test run that should reuse the network created by the first run"
./scripts/tests.e2e.sh --reuse-network --ginkgo.focus-file=xsvm.go "${@}"

SUBSEQUENT_NETWORK_DIR="$(realpath "${SYMLINK_PATH}")"
echo "checking that the symlink path remains the same, indicating that the network was reused"
if [[ "${INITIAL_NETWORK_DIR}" != "${SUBSEQUENT_NETWORK_DIR}" ]]; then
  print_separator
  echo "network was not reused across test runs"
  exit 1
fi
