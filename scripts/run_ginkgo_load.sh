#!/usr/bin/env bash
set -e

# This script assumes that an AvalancheGo and Subnet-EVM binaries are available in the standard location
# within the $GOPATH
# The AvalancheGo and PluginDir paths can be specified via the environment variables used in ./scripts/run.sh.

# Load the versions
SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

source "$SUBNET_EVM_PATH"/scripts/constants.sh

source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Build ginkgo
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@"${GINKGO_VERSION}"

EXTRA_ARGS=()
AVALANCHEGO_BUILD_PATH="${AVALANCHEGO_BUILD_PATH:-}"
if [[ -n "${AVALANCHEGO_BUILD_PATH}" ]]; then
  EXTRA_ARGS=("--avalanchego-path=${AVALANCHEGO_BUILD_PATH}/avalanchego")
  echo "Running with extra args:" "${EXTRA_ARGS[@]}"
fi

ginkgo -vv --label-filter="${GINKGO_LABEL_FILTER:-}" ./tests/load -- "${EXTRA_ARGS[@]}"
