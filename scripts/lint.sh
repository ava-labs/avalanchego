#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -e

if ! [[ "$0" =~ scripts/lint.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

if [ "$#" -eq 0 ]; then
  # by default, check all source code
  # to test only "snow" package
  # ./scripts/lint.sh ./snow/...
  TARGET="./..."
else
  TARGET="${1}"
fi

# by default, "./scripts/lint.sh" runs all lint tests
# to run only "license_header" test
# TESTS='license_header' ./scripts/lint.sh
TESTS=${TESTS:-"golangci_lint license_header"}

function test_golangci_lint {
  go install -v github.com/golangci/golangci-lint/cmd/golangci-lint@v1.43.0
  golangci-lint run --config .golangci.yml
}

# find_go_files [package]
# all go files except generated ones
function find_go_files {
  local target="${1}"
  go fmt -n "${target}"  | grep -Eo "([^ ]*)$" | grep -vE "(\\.pb\\.go|\\.pb\\.gw.go)"
}

# automatically checks license headers
# to modify the file headers (if missing), remove "--check" flag
# TESTS='license_header' ADDLICENSE_FLAGS="-v" ./scripts/lint.sh
_addlicense_flags=${ADDLICENSE_FLAGS:-"--check -v"}
function test_license_header {
  go install -v github.com/google/addlicense@latest
  local target="${1}"
  local files=()
  while IFS= read -r line; do files+=("$line"); done < <(find_go_files "${target}")

  # ignore 3rd party code
  addlicense \
  -f ./LICENSE.header \
  ${_addlicense_flags} \
  --ignore 'utils/ip_test.go' \
  --ignore 'utils/logging/highlight.go' \
  --ignore 'utils/ulimit/ulimit_non_unix.go.go' \
  --ignore 'utils/ulimit/ulimit_unix.go' \
  "${files[@]}"
}

function run {
  local test="${1}"
  shift 1
  echo "START: '${test}' at $(date)"
  if "test_${test}" "$@" ; then
    echo "SUCCESS: '${test}' completed at $(date)"
  else
    echo "FAIL: '${test}' failed at $(date)"
    exit 255
  fi
}

echo "Running '$TESTS' at: $(date)"
for test in $TESTS; do
  run "${test}" "${TARGET}"
done

echo "ALL SUCCESS!"
