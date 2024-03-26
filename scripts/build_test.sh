#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

EXCLUDED_TARGETS="| grep -v /mocks | grep -v proto | grep -v tests/e2e | grep -v tests/upgrade"

GOOS=$(go env GOOS)
if [[ "$GOOS" == "windows" ]]; then
  # tmpnet is not compatible with windows
  EXCLUDED_TARGETS="${EXCLUDED_TARGETS} | grep -v tests/fixture"
fi

TEST_TARGETS="$(eval "go list ./... ${EXCLUDED_TARGETS}")"

# shellcheck disable=SC2086
go test -shuffle=on -race -timeout="${TIMEOUT:-120s}" -coverprofile="coverage.out" -covermode="atomic" ${TEST_TARGETS}
