#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

EXCLUDED_TARGETS="| grep -v /mocks | grep -v proto | grep -v tests/e2e | grep -v tests/load/c | grep -v tests/upgrade | grep -v tests/fixture/bootstrapmonitor/e2e"

if [[ "$(go env GOOS)" == "windows" ]]; then
  # Test discovery for the antithesis test setups is broken due to
  # their dependence on the linux-only Antithesis SDK.
  EXCLUDED_TARGETS="${EXCLUDED_TARGETS} | grep -v tests/antithesis"
fi

TEST_TARGETS="$(eval "go list ./... ${EXCLUDED_TARGETS}")"

# shellcheck disable=SC2086
go test -tags test -shuffle=on -race -timeout="${TIMEOUT:-120s}" -coverprofile="coverage.out" -covermode="atomic" ${TEST_TARGETS}
