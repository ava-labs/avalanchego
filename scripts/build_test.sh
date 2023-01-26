#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

export GOGC=25

# Root directory
SUBNET_EVM_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# Load the versions
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

# We pass in the arguments to this script directly to enable easily passing parameters such as enabling race detection,
# parallelism, and test coverage.
# DO NOT RUN "tests/precompile" or "tests/load" since it's run by ginkgo
go test -coverprofile=coverage.out -covermode=atomic -timeout="30m" $@ $(go list ./... | grep -v tests/precompile | grep -v tests/load)
