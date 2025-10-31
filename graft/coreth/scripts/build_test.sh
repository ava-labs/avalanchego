#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanche root directory
CORETH_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Load the constants
source "$CORETH_PATH"/scripts/constants.sh

# We pass in the arguments to this script directly to enable easily passing parameters such as enabling race detection,
# parallelism, and test coverage.
# DO NOT RUN tests from the top level "tests" directory since they are run by ginkgo
race="-race"
if [[ -n "${NO_RACE:-}" ]]; then
    race=""
fi

# MAX_RUNS bounds the attempts to retry the tests before giving up
# This is useful for flaky tests
MAX_RUNS=4
for ((i = 1; i <= MAX_RUNS; i++));
do
    # shellcheck disable=SC2046
    go test -shuffle=on ${race:-} -timeout="${TIMEOUT:-600s}" -coverprofile=coverage.out -covermode=atomic "$@" $(go list ./... | grep -v github.com/ava-labs/coreth/tests) | tee test.out || command_status=$?

    # If the test passed, exit
    if [[ ${command_status:-0} == 0 ]]; then
        rm test.out
        exit 0
    else 
        unset command_status # Clear the error code for the next run
    fi

    # If the test failed, print the output
    unexpected_failures=$(
        # First grep pattern corresponds to test failures, second pattern corresponds to test panics due to timeouts
        (grep "^--- FAIL" test.out | awk '{print $3}' || grep -E '^\s+Test.+ \(' test.out | awk '{print $1}') |
        sort -u | comm -23 -  <(sed 's/\r$//' ./scripts/known_flakes.txt)
    )
    if [ -n "${unexpected_failures}" ]; then
        echo "Unexpected test failures: ${unexpected_failures}"
        exit 1
    fi

    # Note the absence of unexpected failures cannot be indicative that we only need to run the tests that failed,
    # for example a test may panic and cause subsequent tests in that package to not run.
    # So we loop here.
    echo "Test run $i failed with known flakes, retrying..."
done

# If we reach here, we have failed all retries
exit 1
