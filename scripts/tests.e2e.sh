#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/build.sh
# ./scripts/tests.e2e.sh ./build/avalanchego
if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

#################################
# Sourcing constants.sh ensures that the necessary CGO flags are set to
# build the portable version of BLST. Without this, ginkgo may fail to
# build the test binary if run on a host (e.g. github worker) that lacks
# the instructions to build non-portable BLST.
source ./scripts/constants.sh

#################################
echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/e2e
./tests/e2e/e2e.test --help

#################################
E2E_USE_PERSISTENT_NETWORK="${E2E_USE_PERSISTENT_NETWORK:-}"
TESTNETCTL_NETWORK_DIR="${TESTNETCTL_NETWORK_DIR:-}"
if [[ -n "${E2E_USE_PERSISTENT_NETWORK}" && -n "${TESTNETCTL_NETWORK_DIR}" ]]; then
  echo "running e2e tests against a persistent network configured at ${TESTNETCTL_NETWORK_DIR}"
  E2E_ARGS="--use-persistent-network"
else
  AVALANCHEGO_PATH="${1-${AVALANCHEGO_PATH:-}}"
  if [[ -z "${AVALANCHEGO_PATH}" ]]; then
    echo "Missing AVALANCHEGO_PATH argument!"
    echo "Usage: ${0} [AVALANCHEGO_PATH]" >>/dev/stderr
    exit 255
  fi
  echo "running e2e tests against an ephemeral local cluster deployed with ${AVALANCHEGO_PATH}"
  AVALANCHEGO_PATH="$(realpath ${AVALANCHEGO_PATH})"
  E2E_ARGS="--avalanchego-path=${AVALANCHEGO_PATH}"
fi

#################################
# - Execute in parallel (-p) with the ginkgo cli to minimize execution time.
#   The test binary by itself isn't capable of running specs in parallel.
# - Execute in random order to identify unwanted dependency
ginkgo -p -v --randomize-all ./tests/e2e/e2e.test -- ${E2E_ARGS} \
&& EXIT_CODE=$? || EXIT_CODE=$?

if [[ ${EXIT_CODE} -gt 0 ]]; then
  echo "FAILURE with exit code ${EXIT_CODE}"
  exit ${EXIT_CODE}
else
  echo "ALL SUCCESS!"
fi
