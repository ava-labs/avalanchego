#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/lint.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# The -P option is not supported by the grep version installed by
# default on macos. Since `-o errexit` is ignored in an if
# conditional, triggering the problem here ensures script failure when
# using an unsupported version of grep.
grep -P 'lint.sh' scripts/lint.sh &> /dev/null || (\
  >&2 echo "error: This script requires a recent version of gnu grep, which is available in nix shell.";\
  >&2 echo "       On macos, gnu grep can be installed with 'brew install grep'.";\
  >&2 echo "       It will also be necessary to ensure that gnu grep is available in the path.";\
  exit 255 )

if [ "$#" -eq 0 ]; then
  # by default, check all source code
  # to test only "core" package
  # ./scripts/lint.sh ./core/...
  TARGET="./..."
else
  TARGET="${1}"
fi

# by default, "./scripts/lint.sh" runs golangci-lint and eth imports check
# to run only specific linters, set TESTS environment variable
# TESTS='golangci_lint' ./scripts/lint.sh
TESTS=${TESTS:-"golangci_lint eth_imports"}

function test_golangci_lint {
  go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.63 run --config .golangci.yml
}

function test_eth_imports {
  ./scripts/lint_allowed_eth_imports.sh
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