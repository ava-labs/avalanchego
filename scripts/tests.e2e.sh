#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/tests.e2e.sh
# ./scripts/tests.e2e.sh --ginkgo.label-filter=x               # All arguments are supplied to ginkgo
# E2E_SERIAL=1 ./scripts/tests.e2e.sh                          # Run tests serially
# E2E_RANDOM_SEED=1234882 ./scripts/tests.e2e.sh               # Specify a specific seed to order test execution by
# AVALANCHEGO_PATH=./build/avalanchego ./scripts/tests.e2e.sh  # Customization of avalanchego path
# GINKGO_PATH=/path/to/ginkgo ./scripts/tests.e2e.sh           # Customization of ginkgo path
# E2E_TARGET=/path/to/e2e.test ./scripts/tests.e2e.sh          # Run a prebuilt test binary

if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

E2E_TARGET="${E2E_TARGET:-./tests/e2e}"

#################################
# Source packages need portable BLST compilation settings from constants.sh.
# Skip these settings when Ginkgo runs a prebuilt test binary.
if [[ ! -f "${E2E_TARGET}" ]]; then
  source ./scripts/constants.sh
fi

E2E_ARGS=("${@}")

# If not running in kubernetes, default to using a local avalanchego binary
if [[ ! "${E2E_ARGS[*]}" =~ "--runtime=kube" && ! "${E2E_ARGS[*]}" =~ "--avalanchego-path" ]]; then
  # Ensure an absolute path to avoid dependency on the working directory of script execution.
  AVALANCHEGO_PATH="$(realpath "${AVALANCHEGO_PATH:-./build/avalanchego}")"
  E2E_ARGS+=("--avalanchego-path=${AVALANCHEGO_PATH}")
fi

#################################
# Determine ginkgo args
GINKGO_ARGS=(-v)
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
  GINKGO_ARGS+=(-p)
fi
# Reference: https://onsi.github.io/ginkgo/#spec-randomization
if [[ -n "${E2E_RANDOM_SEED:-}" ]]; then
  # Supply a specific seed to simplify reproduction of test failures
  GINKGO_ARGS+=("--seed=${E2E_RANDOM_SEED}")
else
  # Execute in random order to identify unwanted dependency
  GINKGO_ARGS+=(--randomize-all)
fi

#################################
exec "${GINKGO_PATH:-./bin/ginkgo}" "${GINKGO_ARGS[@]}" "${E2E_TARGET}" -- "${E2E_ARGS[@]}"
