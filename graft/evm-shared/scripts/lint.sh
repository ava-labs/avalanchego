#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/lint.sh ]]; then
  echo "must be run from coreth root"
  exit 255
fi

# The -P option is not supported by the grep version installed by
# default on macos. Since `-o errexit` is ignored in an if
# conditional, triggering the problem here ensures script failure when
# using an unsupported version of grep.
grep -P 'lint.sh' ../evm-shared/scripts/lint.sh &>/dev/null || (
  echo >&2 "error: This script requires a recent version of gnu grep."
  echo >&2 "       On macos, gnu grep can be installed with 'brew install grep'."
  echo >&2 "       It will also be necessary to ensure that gnu grep is available in the path."
  exit 255
)

# Library for file list generation.
source ../evm-shared/scripts/lint_setup.sh

# by default, "./scripts/lint.sh" runs all lint tests
# to run only "license_header" test
# TESTS='license_header' ./scripts/lint.sh
TESTS=${TESTS:-"golangci_lint avalanche_golangci_lint license_header require_error_is_no_funcs_as_params single_import interface_compliance_nil require_no_error_inline_func import_testing_only_in_tests"}

function test_golangci_lint {
  go tool -modfile=../../tools/go.mod golangci-lint run --config .golangci.yml
}

function test_avalanche_golangci_lint {
  if [[ ! -f $AVALANCHE_LINT_FILE ]]; then
    return 0
  fi

  go tool -modfile=../../tools/go.mod golangci-lint run \
  --config "$AVALANCHE_LINT_FILE" \
  || return 1
}

# automatically checks license headers
# to modify the file headers (if missing), remove "--verify" flag
# TESTS='license_header' ADDLICENSE_FLAGS="--debug" ./scripts/lint.sh
_addlicense_flags=${ADDLICENSE_FLAGS:-"--verify --debug"}
function test_license_header {
  # Run license tool
  if [[ ${#UPSTREAM_FILES[@]} -gt 0 ]]; then
    echo "Running license tool on upstream files with header for upstream..."
    # shellcheck disable=SC2086
    go tool -modfile=../../tools/go.mod go-license \
      --config=../../header_upstream.yml \
      ${_addlicense_flags} \
      "${UPSTREAM_FILES[@]}" \
      || return 1
  fi

  if [[ ${#AVALANCHE_FILES[@]} -gt 0 ]]; then
    echo "Running license tool on remaining files with default header..."
    # shellcheck disable=SC2086
    go tool -modfile=../../tools/go.mod go-license \
      --config=../../header.yml \
      ${_addlicense_flags} \
      "${AVALANCHE_FILES[@]}" \
      || return 1
  fi
}

function test_single_import {
  if grep -R -zo -P 'import \(\n\t".*"\n\)' "${AVALANCHE_FILES[@]}"; then
    echo ""
    return 1
  fi
}

function test_require_error_is_no_funcs_as_params {
  if grep -R -zo -P 'require.ErrorIs\(.+?\)[^\n]*\)\n' "${AVALANCHE_FILES[@]}"; then
    echo ""
    return 1
  fi
}

function test_require_no_error_inline_func {
  # Flag only when a single variable whose name contains "err" or "Err"
  # (e.g., err, myErr, parseError) is assigned from a call (:= or =), and later
  # that same variable is passed to require.NoError(...). We explicitly require
  # no commas on the LHS to avoid flagging multi-return assignments like
  # "val, err := f()" or "err, val := f()".
  #
  # Capture the variable name and enforce it matches in the subsequent require.NoError.
  local -r pattern='^\s*([A-Za-z_][A-Za-z0-9_]*[eE]rr[A-Za-z0-9_]*)\s*:?=\s*[^,\n]*\([^)]*\).*\n(?:(?!^\s*(?:if|require)).*\n)*^\s*require\.NoError\((?:t,\s*)?\1\)'
  if grep -R -zo -P "$pattern" "${AVALANCHE_FILES[@]}"; then
    echo ""
    echo "Checking that a function with a single error return doesn't error should be done in-line (single LHS var containing 'err')."
    echo ""
    return 1
  fi
}

# Ref: https://go.dev/doc/effective_go#blank_implements
function test_interface_compliance_nil {
  if grep -R -o -P '_ .+? = &.+?\{\}' "${AVALANCHE_FILES[@]}"; then
    echo ""
    echo "Interface compliance checks need to be of the form:"
    echo "  var _ json.Marshaler = (*RawMessage)(nil)"
    echo ""
    return 1
  fi
}

function test_import_testing_only_in_tests {
  NON_TEST_GO_FILES=$(
    echo "${AVALANCHE_FILES[@]}" | tr ' ' '\n' |
      grep -i '\.go$' |
      grep -vi '_test\.go$' |
      grep -v '^./tests/'
  )

  # Detect which repo we're in based on pwd
  REPO_NAME="avalanchego"
  if [[ "$PWD" == */coreth ]]; then
    REPO_NAME="coreth"
  elif [[ "$PWD" == */subnet-evm ]]; then
    REPO_NAME="subnet-evm"
  fi

  IMPORT_TESTING=$(echo "${NON_TEST_GO_FILES}" | xargs grep -lP '^\s*(import\s+)?"testing"')
  IMPORT_TESTIFY=$(echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/stretchr/testify')
  IMPORT_FROM_TESTS=$(echo "${NON_TEST_GO_FILES}" | xargs grep -lP "\"github.com/ava-labs/(?:avalanchego|${REPO_NAME})/tests/\"")
  IMPORT_TEST_PKG=$(echo "${NON_TEST_GO_FILES}" | xargs grep -lP "\"github.com/ava-labs/(?:avalanchego|${REPO_NAME})/.*?test\"")

  # TODO(arr4n): send a PR to add support for build tags in `mockgen` and then enable this.
  # IMPORT_GOMOCK=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"go.uber.org/mock');
  HAVE_TEST_LOGIC=$(printf "%s\n%s\n%s\n%s" "${IMPORT_TESTING}" "${IMPORT_TESTIFY}" "${IMPORT_FROM_TESTS}" "${IMPORT_TEST_PKG}")

  IN_TEST_PKG=$(echo "${NON_TEST_GO_FILES}" | grep -P '.*test/[^/]+\.go$') # directory (hence package name) ends in "test"
  # Files in /tests/ are already excluded by the `find ... ! -path`
  INTENDED_FOR_TESTING="${IN_TEST_PKG}"

  # -3 suppresses files that have test logic and have the "test" build tag
  # -2 suppresses files that are tagged despite not having detectable test logic
  UNTAGGED=$(comm -23 <(echo "${HAVE_TEST_LOGIC}" | sort -u) <(echo "${INTENDED_FOR_TESTING}" | sort -u))
  if [ -z "${UNTAGGED}" ]; then
    return 0
  fi

  echo 'Non-test Go files importing test-only packages MUST (a) be in *test package; or (b) be in /tests/ directory:'
  echo "${UNTAGGED}"
  return 1
}

function run {
  local test="${1}"
  shift 1
  echo "START: '${test}' at $(date)"
  
  # Filter out files that have skiplint comments for this specific test
  local filtered_files=()
  for file in "$@"; do
    # Check if file has skiplint comment for this test
    if ! grep -q "// #skiplint: ${test}" "$file" 2>/dev/null; then
      filtered_files+=("$file")
    fi
  done

  if [ ${#filtered_files[@]} -eq 0 ]; then
    echo "SKIPPED: '${test}' - No files remain after filtering at $(date)"
    return 0
  fi
  
  if "test_${test}" "${filtered_files[@]}"; then
    echo "SUCCESS: '${test}' completed at $(date)"
  else
    echo "FAIL: '${test}' failed at $(date)"
    exit 255
  fi
}

echo "Running '$TESTS' at: $(date)"
setup_lint
for test in $TESTS; do
  run "${test}" "${AVALANCHE_FILES[@]}"
done

echo "ALL SUCCESS!"

