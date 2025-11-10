#!/usr/bin/env bash

set -euo pipefail

# This script runs only the unit tests for coreth to allow setting a
# longer default timeout to account for its legacy unit tests not
# being compatible with the shorter 2m timeout expected of existing
# tests.

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
source "$REPO_ROOT"/scripts/constants.sh

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

# shellcheck disable=SC2046
go test -tags ${shuffle:-} ${race:-} -timeout="${TIMEOUT:-600s}" -coverprofile=coverage.out -covermode=atomic "$@" $(go list ./graft/coreth/... | grep -v graft/coreth/tests)
