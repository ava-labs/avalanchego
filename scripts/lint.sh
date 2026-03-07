#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/lint.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Root-level directories to exclude from linting
EXCLUDE_DIRS=(
  graft    # Grafted modules have their own lint config
  .direnv  # direnv cache
  .idea    # GoLand
)

# Pre-build find exclusion args (root-level only)
FIND_EXCLUDES=()
for dir in "${EXCLUDE_DIRS[@]}"; do
  FIND_EXCLUDES+=(! -path "./${dir}/*")
done

# shellcheck disable=SC1091
source ./scripts/vcs.sh

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
# to run only "license_header" test
# TESTS='license_header' ./scripts/lint.sh
TESTS=${TESTS:-"golangci_lint warn_testify_assert license_header require_error_is_no_funcs_as_params single_import interface_compliance_nil require_no_error_inline_func import_testing_only_in_tests"}

function test_golangci_lint {
  ./scripts/run_tool.sh golangci-lint run --config .golangci.yml
}

# Source shared helper for testify/assert warnings (provides test_warn_testify_assert)
# shellcheck disable=SC1091
source ./scripts/lint_warn_assert.sh

# automatically checks license headers
# to modify the file headers (if missing), remove "--verify" flag
# TESTS='license_header' ADDLICENSE_FLAGS="--debug" ./scripts/lint.sh
_addlicense_flags=${ADDLICENSE_FLAGS:-"--verify --debug"}
function test_license_header {
  local files=()
  while IFS= read -r line; do files+=("$line"); done < <(
    find . -type f -name '*.go' \
      ! -name '*.pb.go' \
      ! -name '*.connect.go' \
      ! -name 'mock_*.go' \
      ! -name 'mocks_*.go' \
      ! -path './**/*mock/*.go' \
      ! -name '*.canoto.go' \
      ! -name '*.bindings.go' \
      "${FIND_EXCLUDES[@]}"
    )

  # shellcheck disable=SC2086
  ./scripts/run_tool.sh go-license \
  --config=./header.yml \
  ${_addlicense_flags} \
  "${files[@]}"
}

function test_single_import {
  if find . -type f -name '*.go' "${FIND_EXCLUDES[@]}" -print0 | xargs -0 grep -zo -P 'import \(\n\t".*"\n\)'; then
    echo ""
    return 1
  fi
}

function test_require_error_is_no_funcs_as_params {
  if find . -type f -name '*.go' "${FIND_EXCLUDES[@]}" -print0 | xargs -0 grep -zo -P 'require.ErrorIs\(.+?\)[^\n]*\)\n'; then
    echo ""
    return 1
  fi
}

function test_require_no_error_inline_func {
  if find . -type f -name '*.go' "${FIND_EXCLUDES[@]}" -print0 | xargs -0 grep -zo -P '\t+err :?= ((?!require|if).|\n)*require\.NoError\((t, )?err\)'; then
    echo ""
    echo "Checking that a function with a single error return doesn't error should be done in-line."
    echo ""
    return 1
  fi
}

# Ref: https://go.dev/doc/effective_go#blank_implements
function test_interface_compliance_nil {
  if find . -type f -name '*.go' "${FIND_EXCLUDES[@]}" -print0 | xargs -0 grep -o -P '_ .+? = &.+?\{\}'; then
    echo ""
    echo "Interface compliance checks need to be of the form:"
    echo "  var _ json.Marshaler = (*RawMessage)(nil)"
    echo ""
    return 1
  fi
}

function test_import_testing_only_in_tests {
  ROOT=$( vcs_repo_root )
  # Build exclusions with absolute paths (ROOT prefix)
  local exclude_paths=(! -path "${ROOT}/tests/*")
  for dir in "${EXCLUDE_DIRS[@]}"; do
    exclude_paths+=(! -path "${ROOT}/${dir}/*")
  done
  NON_TEST_GO_FILES=$( find "${ROOT}" -iname '*.go' ! -iname '*_test.go' "${exclude_paths[@]}" );

  IMPORT_TESTING=$( echo "${NON_TEST_GO_FILES}" | xargs grep -lP '^\s*(import\s+)?"testing"');
  IMPORT_TESTIFY=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/stretchr/testify');
  IMPORT_FROM_TESTS=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/ava-labs/avalanchego/.*?tests/');
  IMPORT_TEST_PKG=$( echo "${NON_TEST_GO_FILES}" | xargs grep -lP '"github.com/ava-labs/avalanchego/.*?test"');

  # TODO(arr4n): send a PR to add support for build tags in `mockgen` and then enable this.
  # IMPORT_GOMOCK=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"go.uber.org/mock');
  HAVE_TEST_LOGIC=$( printf "%s\n%s\n%s\n%s" "${IMPORT_TESTING}" "${IMPORT_TESTIFY}" "${IMPORT_FROM_TESTS}" "${IMPORT_TEST_PKG}" );

  IN_TEST_PKG=$( echo "${NON_TEST_GO_FILES}" | grep -P '.*test/[^/]+\.go$' ) # directory (hence package name) ends in "test"

  # Files in /tests/ are already excluded by the `find ... ! -path`
  INTENDED_FOR_TESTING="${IN_TEST_PKG}"

  # -3 suppresses files that have test logic and have the "test" build tag
  # -2 suppresses files that are tagged despite not having detectable test logic
  UNTAGGED=$( comm -23 <( echo "${HAVE_TEST_LOGIC}" | sort -u ) <( echo "${INTENDED_FOR_TESTING}" | sort -u ) );
  if [ -z "${UNTAGGED}" ];
  then
    return 0;
  fi

  echo 'Non-test Go files importing test-only packages MUST (a) be in *test package; or (b) be in /tests/ directory:';
  echo "${UNTAGGED}";
  return 1;
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
