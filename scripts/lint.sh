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
# to run only "license_header" test
# TESTS='license_header' ./scripts/lint.sh
TESTS=${TESTS:-"golangci_lint license_header require_error_is_no_funcs_as_params single_import interface_compliance_nil require_no_error_inline_func import_testing_only_in_tests"}

function test_golangci_lint {
  go install -v github.com/golangci/golangci-lint/cmd/golangci-lint@v1.59.1
  golangci-lint run --config .golangci.yml
}

# automatically checks license headers
# to modify the file headers (if missing), remove "--verify" flag
# TESTS='license_header' ADDLICENSE_FLAGS="--debug" ./scripts/lint.sh
_addlicense_flags=${ADDLICENSE_FLAGS:-"--verify --debug"}
function test_license_header {
  go install -v github.com/palantir/go-license@v1.25.0
  local files=()
  while IFS= read -r line; do files+=("$line"); done < <(find . -type f -name '*.go' ! -name '*.pb.go' ! -name 'mock_*.go')

  # shellcheck disable=SC2086
  go-license \
  --config=./header.yml \
  ${_addlicense_flags} \
  "${files[@]}"
}

function test_single_import {
  if grep -R -zo -P 'import \(\n\t".*"\n\)' .; then
    echo ""
    return 1
  fi
}

function test_require_error_is_no_funcs_as_params {
  if grep -R -zo -P 'require.ErrorIs\(.+?\)[^\n]*\)\n' .; then
    echo ""
    return 1
  fi
}

function test_require_no_error_inline_func {
  if grep -R -zo -P '\t+err :?= ((?!require|if).|\n)*require\.NoError\((t, )?err\)' .; then
    echo ""
    echo "Checking that a function with a single error return doesn't error should be done in-line."
    echo ""
    return 1
  fi
}

# Ref: https://go.dev/doc/effective_go#blank_implements
function test_interface_compliance_nil {
  if grep -R -o -P '_ .+? = &.+?\{\}' .; then
    echo ""
    echo "Interface compliance checks need to be of the form:"
    echo "  var _ json.Marshaler = (*RawMessage)(nil)"
    echo ""
    return 1
  fi
}

function test_import_testing_only_in_tests {
  ROOT=$( git rev-parse --show-toplevel )
  NON_TEST_GO_FILES=$( find "${ROOT}" -iname '*.go' ! -iname '*_test.go');

  IMPORT_TESTING=$( echo "${NON_TEST_GO_FILES}" | xargs grep -lP '^\s*(import\s+)?"testing"');
  IMPORT_TESTIFY=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/stretchr/testify');
  # TODO(arr4n): send a PR to add support for build tags in `mockgen` and then enable this.
  # IMPORT_GOMOCK=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"go.uber.org/mock');
  HAVE_TEST_LOGIC=$( printf "%s\n%s" "${IMPORT_TESTING}" "${IMPORT_TESTIFY}" );

  TAGGED_AS_TEST=$( echo "${NON_TEST_GO_FILES}" | xargs grep -lP '^\/\/go:build\s+(.+(,|\s+))?test[,\s]?');

  # -3 suppresses files that have test logic and have the "test" build tag
  # -2 suppresses files that are tagged despite not having detectable test logic
  UNTAGGED=$( comm -23 <( echo "${HAVE_TEST_LOGIC}" | sort -u ) <( echo "${TAGGED_AS_TEST}" | sort -u ) );
  if [ -z "${UNTAGGED}" ];
  then
    return 0;
  fi

  echo "Non-test Go files importing test-only packages MUST have '//go:build test' tag:";
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
