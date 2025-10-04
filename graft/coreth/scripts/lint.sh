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

# Read excluded directories into arrays
DEFAULT_FILES=()
UPSTREAM_FILES=()
AVALANCHE_LINT_FILE=""
function read_dirs {
  local upstream_folders_file="./scripts/upstream_files.txt"
  # Read the upstream_folders file into an array
  mapfile -t upstream_folders <"$upstream_folders_file"

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
  local -a upstream_find_args=()
  local -a upstream_exclude_args=()
  for line in "${upstream_folders[@]}"; do
    # Skip empty lines
    [[ -z "$line" ]] && continue

    if [[ "$line" == !* ]]; then
      # Excluding files with !
      upstream_exclude_args+=(! -path "./${line:1}")
    else
      upstream_find_args+=(-path "./${line}" -o)
    fi
  done
  # Remove the last '-o' from the arrays
  unset 'upstream_find_args[${#upstream_find_args[@]}-1]'

  # Find upstream files
  mapfile -t UPSTREAM_FILES < <(
    find . \
      \( "${upstream_find_args[@]}" \) \
      -a \( "${find_filters[@]}" \) \
      "${upstream_exclude_args[@]}"
  )

  # Build exclusion args from upstream files
  default_exclude_args=()
  for f in "${UPSTREAM_FILES[@]}"; do
    default_exclude_args+=(! -path "$f")
  done

  # Now find default files (exclude already licensed ones)
  mapfile -t DEFAULT_FILES < <(find . "${find_filters[@]}" "${default_exclude_args[@]}")

  # copy avalanche file to temp directory to edit
  AVALANCHE_LINT_FILE="$(mktemp -d)/.avalanche-golangci.yml"
  echo "Avalanche lint file at: $AVALANCHE_LINT_FILE"
  cp .avalanche-golangci.yml "$AVALANCHE_LINT_FILE"

  # Exclude all upstream files dynamically
  echo "    paths-except:" >> "$AVALANCHE_LINT_FILE"
  for f in "${UPSTREAM_FILES[@]}"; do
    # exclude pre-pended "./"
    echo "      - \"${f:2}\$\"" >> "$AVALANCHE_LINT_FILE"
  done
}

# by default, "./scripts/lint.sh" runs all lint tests
# to run only "license_header" test
# TESTS='license_header' ./scripts/lint.sh
TESTS=${TESTS:-"golangci_lint avalanche_golangci_lint license_header require_error_is_no_funcs_as_params single_import interface_compliance_nil require_no_error_inline_func import_testing_only_in_tests"}

function test_golangci_lint {
  # Since there are 2 versions of golangci-lint in play, and only one
  # can be managed by a given go.mod file, we use a separate mod file
  # for the older version.
  # TODO(marun) Switch everything to v2 when possible
  go tool -modfile=tools/legacy-golangci-lint.mod golangci-lint run --config .golangci.yml
}

function test_avalanche_golangci_lint {
  if [[ ! -f $AVALANCHE_LINT_FILE ]]; then
    return 0
  fi

  go tool -modfile=tools/go.mod golangci-lint run \
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
    go tool -modfile=tools/go.mod go-license \
      --config=./license_header_for_upstream.yml \
      ${_addlicense_flags} \
      "${UPSTREAM_FILES[@]}" \
      || return 1
  fi

  if [[ ${#DEFAULT_FILES[@]} -gt 0 ]]; then
    echo "Running license tool on remaining files with default header..."
    # shellcheck disable=SC2086
    go tool -modfile=tools/go.mod go-license \
      --config=./license_header.yml \
      ${_addlicense_flags} \
      "${DEFAULT_FILES[@]}" \
      || return 1
  fi
}

function test_single_import {
  if grep -R -zo -P 'import \(\n\t".*"\n\)' "${DEFAULT_FILES[@]}"; then
    echo ""
    return 1
  fi
}

function test_require_error_is_no_funcs_as_params {
  if grep -R -zo -P 'require.ErrorIs\(.+?\)[^\n]*\)\n' "${DEFAULT_FILES[@]}"; then
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
  if grep -R -zo -P "$pattern" "${DEFAULT_FILES[@]}"; then
    echo ""
    echo "Checking that a function with a single error return doesn't error should be done in-line (single LHS var containing 'err')."
    echo ""
    return 1
  fi
}

# Ref: https://go.dev/doc/effective_go#blank_implements
function test_interface_compliance_nil {
  if grep -R -o -P '_ .+? = &.+?\{\}' "${DEFAULT_FILES[@]}"; then
    echo ""
    echo "Interface compliance checks need to be of the form:"
    echo "  var _ json.Marshaler = (*RawMessage)(nil)"
    echo ""
    return 1
  fi
}

function test_import_testing_only_in_tests {
  NON_TEST_GO_FILES=$(
    echo "${DEFAULT_FILES[@]}" | tr ' ' '\n' |
      grep -i '\.go$' |
      grep -vi '_test\.go$' |
      grep -v '^./tests/'
  )

  IMPORT_TESTING=$(echo "${NON_TEST_GO_FILES}" | xargs grep -lP '^\s*(import\s+)?"testing"')
  IMPORT_TESTIFY=$(echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/stretchr/testify')
  IMPORT_FROM_TESTS=$(echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/ava-labs/coreth/tests/')
  IMPORT_TEST_PKG=$(echo "${NON_TEST_GO_FILES}" | xargs grep -lP '"github.com/ava-labs/coreth/.*?test"')

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
  if "test_${test}" "$@"; then
    echo "SUCCESS: '${test}' completed at $(date)"
  else
    echo "FAIL: '${test}' failed at $(date)"
    exit 255
  fi
}

echo "Running '$TESTS' at: $(date)"
read_dirs
for test in $TESTS; do
  run "${test}" "${DEFAULT_FILES[@]}"
done

echo "ALL SUCCESS!"
