#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# Run the tests with shuffling unless NO_SHUFFLE is set.
shuffle="-shuffle=on"
if [[ -n "${NO_SHUFFLE:-}" ]]; then
    shuffle=""
fi

# Run the tests with race detection unless NO_RACE is set.
race="-race"
if [[ -n "${NO_RACE:-}" ]]; then
    race=""
fi

EXCLUDED_TARGETS="|\
 grep -v /mocks |\
 grep -v proto |\
 grep -v tests/e2e |\
 grep -v tests/load/c |\
 grep -v tests/upgrade |\
 grep -v tests/fixture/bootstrapmonitor/e2e |\
 grep -v tests/reexecute |\
 grep -v graft/coreth"

TEST_TARGETS="$(eval "go list ./... ${EXCLUDED_TARGETS}")"

# shellcheck disable=SC2086
go test -tags test ${shuffle:-} ${race:-} -timeout="${TIMEOUT:-120s}" -coverprofile="coverage.out" -covermode="atomic" ${TEST_TARGETS}
