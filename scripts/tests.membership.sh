#!/usr/bin/env bash

set -euo pipefail

# Runs the certificate-based subnet membership e2e test. Every argument is
# passed through to the test binary, e.g.
#
#   ./scripts/tests.membership.sh
#   AVALANCHEGO_PATH=./build/avalanchego ./scripts/tests.membership.sh
if ! [[ "$0" =~ scripts/tests.membership.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Sourcing constants.sh ensures that the necessary CGO flags are set to build
# the portable version of BLST.
source ./scripts/constants.sh

TEST_ARGS=("${@}")

if ! [[ "${TEST_ARGS[*]}" =~ "--avalanchego-path" ]]; then
  # Ensure an absolute path to avoid dependency on the working directory of script execution.
  AVALANCHEGO_PATH="$(realpath "${AVALANCHEGO_PATH:-./build/avalanchego}")"
  TEST_ARGS+=("--avalanchego-path=${AVALANCHEGO_PATH}")
fi

# The suite is a single spec sharing one network, so there is nothing to
# parallelize. -v keeps each step on screen to locate a failure.
./bin/ginkgo -v ./tests/membership -- "${TEST_ARGS[@]}"
