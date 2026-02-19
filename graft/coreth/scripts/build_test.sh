#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../../../ && pwd )
# shellcheck disable=SC1091
source "$REPO_ROOT"/scripts/constants.sh

# We pass in the arguments to this script directly to enable easily passing parameters such as enabling race detection,
# parallelism, and test coverage.
# DO NOT RUN tests from the top level "tests" directory since they are run by ginkgo
race="-race"
if [[ -n "${NO_RACE:-}" ]]; then
    race=""
fi

cd "$REPO_ROOT/graft/coreth"
# shellcheck disable=SC2046
bisect -compile=variablemake -godebug checkfinalizers=1 go test -shuffle=on ${race:-} -timeout="${TIMEOUT:-900s}" -coverprofile=coverage.out -covermode=atomic "$@" $(go list .//... | grep -v github.com/ava-labs/avalanchego/graft/coreth/tests)
