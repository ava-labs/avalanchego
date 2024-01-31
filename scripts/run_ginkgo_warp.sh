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
go install -v "github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION}"

ACK_GINKGO_RC=true ginkgo build ./tests/warp

./tests/warp/warp.test \
  --ginkgo.vv \
  --ginkgo.label-filter="${GINKGO_LABEL_FILTER:-""}"
