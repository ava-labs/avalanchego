#!/usr/bin/env bash

set -euo pipefail

################################################################
# This script deploys an ephemeral network and configures
# tests.e2e.sh to execute the e2e suite against it. This
# validates that ephnetctl is capable of starting a network and
# that the e2e suite is capable of executing against a network
# that it did not create.
################################################################

# e.g.,
# ./scripts/build.sh
# ./scripts/tests.e2e.existing.sh --ginkgo.label-filter=x               # All arguments are supplied to ginkgo
# E2E_SERIAL=1 ./scripts/tests.e2e.sh                                   # Run tests serially
# AVALANCHEGO_PATH=./build/avalanchego ./scripts/tests.e2e.existing.sh  # Customization of avalanchego path
if ! [[ "$0" =~ scripts/tests.e2e.existing.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Ensure an absolute path to avoid dependency on the working directory
# of script execution.
export AVALANCHEGO_PATH="$(realpath ${AVALANCHEGO_PATH:-./build/avalanchego})"

# Provide visual separation between testing and setup/teardown
function print_separator {
  printf '%*s\n' "${COLUMNS:-80}" '' | tr ' ' ─
}

# Ensure network cleanup on teardown
function cleanup {
  print_separator
  echo "cleaning up ephemeral network"
  if [[ -n "${EPHNET_NETWORK_DIR:-}" ]]; then
    ./build/ephnetctl stop-network
  fi
}
trap cleanup EXIT

# Start an ephemeral network
./scripts/build_ephnetctl.sh
print_separator
./build/ephnetctl start-network

# Determine the network configuration path from the latest symlink
LATEST_SYMLINK_PATH="${HOME}/.ephnet/networks/latest"
if [[ -h "${LATEST_SYMLINK_PATH}" ]]; then
  export EPHNET_NETWORK_DIR="$(realpath ${LATEST_SYMLINK_PATH})"
else
  echo "failed to find configuration path: ${LATEST_SYMLINK_PATH} symlink not found"
  exit 255
fi

print_separator
# - Setting E2E_USE_EXISTING_NETWORK configures tests.e2e.sh to use
#   the ephemeral network identified by EPHNET_NETWORK_DIR.
# - Only a single test (selected with --ginkgo.focus-file) is required
#   to validate that an existing network can be used by an e2e test
#   suite run. Executing more tests would be duplicative of the testing
#   performed against a network created by the test suite.
E2E_USE_EXISTING_NETWORK=1 ./scripts/tests.e2e.sh --ginkgo.focus-file=permissionless_subnets.go
