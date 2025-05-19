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
grep -P 'lint.sh' scripts/lint.sh &>/dev/null || (
  echo >&2 "error: This script requires a recent version of gnu grep."
  echo >&2 "       On macos, gnu grep can be installed with 'brew install grep'."
  echo >&2 "       It will also be necessary to ensure that gnu grep is available in the path."
  exit 255
)

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
  go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.63 run --config .golangci.yml
}

# automatically checks license headers
# to modify the file headers (if missing), remove "--verify" flag
# TESTS='license_header' ADDLICENSE_FLAGS="--debug" ./scripts/lint.sh
_addlicense_flags=${ADDLICENSE_FLAGS:-"--verify --debug"}
function test_license_header {
  local upstream_licensed_folders_file="./scripts/upstream_licensed_folders.txt"
  # Read the upstream_licensed_folders file into an array
  mapfile -t upstream_licensed_folders <"$upstream_licensed_folders_file"

  local upstream_licensed_files=()
  local default_files=()

  # Shared find filters
  local -a find_filters=(
    -type f
    -name '*.go'
    ! -name '*.pb.go'
    ! -name 'mock_*.go'
    ! -name 'mocks_*.go'
    ! -name 'mocks.go'
    ! -name 'mock.go'
    ! -name 'gen_*.go'
    ! -path './**/*mock/*.go'
  )

  # Combined loop: build both upstream licensed find and exclude args
  local -a upstream_licensed_find_args=()
  local -a upstream_licensed_exclude_args=()
  for line in "${upstream_licensed_folders[@]}"; do
    if [[ "$line" == !* ]]; then
      # Excluding files with ! only works for files, not directories
      upstream_licensed_exclude_args+=(! -path "./${line:1}")
    else
      upstream_licensed_find_args+=(-path "./${line}" -o)
    fi
  done
  # Remove the last '-o' from the arrays
  unset 'upstream_licensed_find_args[${#upstream_licensed_find_args[@]}-1]'

  # Find upstream_licensed files
  mapfile -t upstream_licensed_files < <(
    find . \
      \( "${upstream_licensed_find_args[@]}" \) \
      -a \( "${find_filters[@]}" \) \
      "${upstream_licensed_exclude_args[@]}"
  )

  # Build exclusion args from upstream files
  default_exclude_args=()
  for f in "${upstream_licensed_files[@]}"; do
    default_exclude_args+=(! -path "$f")
  done

  # Now find default files (exclude already licensed ones)
  mapfile -t default_files < <(find . "${find_filters[@]}" "${default_exclude_args[@]}")

  # Run license tool
  if [[ ${#upstream_licensed_files[@]} -gt 0 ]]; then
    echo "Running license tool on upstream_licensed files with header for upstream..."
    # shellcheck disable=SC2086
    go run github.com/palantir/go-license@v1.25.0 \
      --config=./license_header_for_upstream.yml \
      ${_addlicense_flags} \
      "${upstream_licensed_files[@]}"
  fi

  if [[ ${#default_files[@]} -gt 0 ]]; then
    echo "Running license tool on remaining files with default header..."
    # shellcheck disable=SC2086
    go run github.com/palantir/go-license@v1.25.0 \
      --config=./license_header.yml \
      ${_addlicense_flags} \
      "${default_files[@]}"
  fi
}

function run {
  local test="${1}"
  shift 1
  echo "START: '${test}' at $(date)"
  if "test_${test}" "$@"; then
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
