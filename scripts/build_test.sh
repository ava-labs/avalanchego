#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

export GOGC=25

# TODO(marun) Ensure the working directory is the repository root or a non-canonical set of tests may be executed

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
# DO NOT RUN tests from the top level "tests" directory since they are run by ginkgo
# shellcheck disable=SC2046
go test -coverprofile=coverage.out -covermode=atomic -timeout="30m" "$@" $(go list ./... | grep -v github.com/ava-labs/subnet-evm/tests)
