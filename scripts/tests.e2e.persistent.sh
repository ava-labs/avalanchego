#!/usr/bin/env bash

set -e
set -o nounset
set -o pipefail

################################################################
# This script deploys a persistent local network and configures
# tests.e2e.sh to execute the e2e suite against it.
################################################################

# e.g.,
# ./scripts/build.sh
# ./scripts/tests.e2e.persistent_network.sh ./build/avalanchego
if ! [[ "$0" =~ scripts/tests.e2e.persistent.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

AVALANCHEGO_PATH="${1-${AVALANCHEGO_PATH:-}}"
if [[ -z "${AVALANCHEGO_PATH}" ]]; then
  echo "Missing AVALANCHEGO_PATH argument!"
  echo "Usage: ${0} [AVALANCHEGO_PATH]" >>/dev/stderr
  exit 255
fi
# Ensure an absolute path to avoid dependency on the working directory
# of script execution.
export AVALANCHEGO_PATH="$(realpath ${AVALANCHEGO_PATH})"

# Create a temporary directory to store persistent network
ROOT_DIR="$(mktemp -d -t e2e-testnet.XXXXX)"

# Provide visual separation between testing and setup/teardown
function print_separator {
  printf '%*s\n' "${COLUMNS:-80}" '' | tr ' ' ─
}

# Ensure network cleanup on teardown
function cleanup {
  print_separator
  echo "cleaning up persistent network"
  if [[ -n "${TESTNETCTL_NETWORK_DIR:-}" ]]; then
    ./build/testnetctl stop-network
  fi
  rm -r "${ROOT_DIR}"
}
trap cleanup EXIT

# Start a persistent network
./scripts/build_testnetctl.sh
print_separator
./build/testnetctl start-network --root-dir="${ROOT_DIR}"

# Determine the network configuration path from the latest symlink
LATEST_SYMLINK_PATH="${ROOT_DIR}/latest"
if [[ -h "${LATEST_SYMLINK_PATH}" ]]; then
  export TESTNETCTL_NETWORK_DIR="$(realpath ${LATEST_SYMLINK_PATH})"
else
  echo "failed to find configuration path: ${LATEST_SYMLINK_PATH} symlink not found"
  exit 255
fi

print_separator
# TODO(marun) Enable e2e testing
