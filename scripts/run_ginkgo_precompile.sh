#!/usr/bin/env bash
set -e

# This script assumes that an AvalancheGo and Subnet-EVM binaries are available in the standard location
# within the $GOPATH
# The AvalancheGo and PluginDir paths can be specified via the environment variables used in ./scripts/run.sh.

SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

source "$SUBNET_EVM_PATH"/scripts/constants.sh

TEST_SOURCE_ROOT=$(pwd)

# By default, it runs all e2e test cases!
# Use "--ginkgo.skip" to skip tests.
# Use "--ginkgo.focus" to select tests.
TEST_SOURCE_ROOT="$TEST_SOURCE_ROOT" "${SUBNET_EVM_PATH}"/bin/ginkgo run -procs=5 tests/precompile \
  --ginkgo.vv \
  --ginkgo.label-filter="${GINKGO_LABEL_FILTER:-""}"
