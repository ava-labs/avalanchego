#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/tests.e2e.sh
# ./scripts/tests.e2e.sh --ginkgo.label-filter=x                                       # All arguments are supplied to ginkgo
# E2E_SERIAL=1 ./scripts/tests.e2e.sh                                                  # Run tests serially
# AVALANCHEGO_PATH=./build/avalanchego ./scripts/tests.e2e.sh                          # Customization of avalanchego path
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
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.13.1
ACK_GINKGO_RC=true ginkgo build ./tests/e2e
./tests/e2e/e2e.test --help

# Enable subnet testing by building xsvm
./scripts/build_xsvm.sh
echo ""

# Ensure an absolute path to avoid dependency on the working directory
# of script execution.
AVALANCHEGO_PATH="$(realpath "${AVALANCHEGO_PATH:-./build/avalanchego}")"
E2E_ARGS="--avalanchego-path=${AVALANCHEGO_PATH}"

#################################
# Determine ginkgo args
GINKGO_ARGS=""
if [[ -n "${E2E_SERIAL:-}" ]]; then
  # Specs will be executed serially. This supports running e2e tests in CI
  # where parallel execution of tests that start new nodes beyond the
  # initial set of validators could overload the free tier CI workers.
  # Forcing serial execution in this test script instead of marking
  # resource-hungry tests as serial supports executing the test suite faster
  # on powerful development workstations.
  echo "tests will be executed serially to minimize resource requirements"
else
  # Enable parallel execution of specs defined in the test binary by
  # default. This requires invoking the binary via the ginkgo cli
  # since the test binary isn't capable of executing specs in
  # parallel.
  echo "tests will be executed in parallel"
  GINKGO_ARGS="-p"
fi

#################################
# - Execute in random order to identify unwanted dependency
ginkgo ${GINKGO_ARGS} -v --randomize-all ./tests/e2e/e2e.test -- "${E2E_ARGS[@]}" "${@}"
