#!/usr/bin/env bash

set -euo pipefail

# This script runs only the unit tests for coreth to allow setting a
# longer default timeout to account for its legacy unit tests not
# being compatible with the shorter 2m timeout expected of existing
# tests.

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
source "$REPO_ROOT"/scripts/constants.sh

# We pass in the arguments to this script directly to enable easily passing parameters such as enabling race detection,
# parallelism, and test coverage.
# DO NOT RUN tests from the top level "tests" directory since they are run by ginkgo
race="-race"
if [[ -n "${NO_RACE:-}" ]]; then
    race=""
fi

# shellcheck disable=SC2046
go test -shuffle=on ${race:-} -timeout="${TIMEOUT:-600s}" -coverprofile=coverage.out -covermode=atomic "$@" $(go list ./graft/coreth/... | grep -v graft/coreth/tests)
