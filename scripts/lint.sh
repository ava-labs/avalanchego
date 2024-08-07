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
  >&2 echo "error: This script requires a recent version of gnu grep.";\
  >&2 echo "       On macos, gnu grep can be installed with 'brew install grep'.";\
  >&2 echo "       It will also be necessary to ensure that gnu grep is available in the path.";\
  exit 255 )

if [ "$#" -eq 0 ]; then
  # by default, check all source code
  # to test only "snow" package
  # ./scripts/lint.sh ./snow/...
  TARGET="./..."
else
  TARGET="${1}"
fi

# by default, "./scripts/lint.sh" runs all lint tests
TESTS=${TESTS:-"golangci_lint license_header"}

# to run only "golangci_lint" test
# TESTS='golangci_lint' ./scripts/lint.sh
function test_golangci_lint {
  go install -v github.com/golangci/golangci-lint/cmd/golangci-lint@v1.51.2
  golangci-lint run --config .golangci.yml
}

# automatically checks license headers
# to modify the file headers (if missing), remove "--verify" flag
# TESTS='license_header' ./scripts/lint.sh
function test_license_header {
  go install -v github.com/chain4travel/camino-license@v0.0.1
  # TODO: use directory instead of files and do these exclusions from camino-license configuration
  local files=()
  while IFS= read -r line; do files+=("$line"); done < <(find . -type f -name '*.go' ! -name '*.pb.go' ! -name 'mock_*.go' ! -name 'camino_mock*.go' |\
    grep -v '^./ipcs/socket/socket_unix.go' |\
    grep -v '^./ipcs/socket/socket_windows.go' |\
    grep -v '^./utils/linkedhashmap/linkedhashmap.go' |\
    grep -v '^./utils/linkedhashmap/iterator.go' |\
    grep -v '^./utils/storage/storage_unix.go' |\
    grep -v '^./utils/storage/storage_windows.go' |\
    grep -v '^./vms/rpcchainvm/runtime/subprocess/non_linux_stopper.go' |\
    grep -v '^./vms/rpcchainvm/runtime/subprocess/linux_stopper.go' |\
    grep -v '^./tools/camino-network-runner/')

  # shellcheck disable=SC2086
  camino-license check\
  --config=./header.yaml \
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
