#!/usr/bin/env bash

set -euo pipefail

# This script verifies that a network can be reused across test runs.

# e.g.,
# ./scripts/build.sh
# AVALANCHEGO_PATH=./build/avalanchego ./scripts/tests.e2e.existing.sh  # Customization of avalanchego path
if ! [[ "$0" =~ scripts/tests.e2e.existing.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Ensure an absolute path to avoid dependency on the working directory
# of script execution.
AVALANCHEGO_PATH="$(realpath "${AVALANCHEGO_PATH:-./build/avalanchego}")"
export AVALANCHEGO_PATH

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

echo "building e2e.test"
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.13.1
ACK_GINKGO_RC=true ginkgo build ./tests/e2e

print_separator
echo "starting initial test run that should create the reusable network"
ginkgo -v ./tests/e2e/e2e.test -- --reuse-network --ginkgo.focus-file=permissionless_subnets.go

print_separator
echo "determining the network path of the reusable network created by the first test run"
SYMLINK_PATH="${HOME}/.tmpnet/networks/latest_avalanchego-e2e"
INITIAL_NETWORK_DIR="$(realpath "${SYMLINK_PATH}")"

print_separator
echo "starting second test run that should reuse the network created by the first run"
ginkgo -v ./tests/e2e/e2e.test -- --reuse-network --ginkgo.focus-file=permissionless_subnets.go


SUBSEQUENT_NETWORK_DIR="$(realpath "${SYMLINK_PATH}")"
echo "checking that the symlink path remains the same, indicating that the network was reused"
if [[ "${INITIAL_NETWORK_DIR}" != "${SUBSEQUENT_NETWORK_DIR}" ]]; then
  print_separator
  echo "network was not reused across test runs"
  exit 1
fi
