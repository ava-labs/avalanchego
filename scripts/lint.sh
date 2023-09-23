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
TESTS=${TESTS:-"golangci_lint license_header require_error_is_no_funcs_as_params single_import interface_compliance_nil require_equal_zero require_len_zero require_equal_len require_nil require_no_error_inline_func"}

function test_golangci_lint {
  go install -v github.com/golangci/golangci-lint/cmd/golangci-lint@v1.54.2
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

function test_require_equal_zero {
  # check if the first arg, other than t, is 0
  if grep -R -o -P 'require\.Equal\((t, )?(u?int\d*\(0\)|0)' .; then
    echo ""
    echo "Use require.Zero instead of require.Equal when testing for 0."
    echo ""
    return 1
  fi

  # check if the last arg is 0
  if grep -R -zo -P 'require\.Equal\(.+?, (u?int\d*\(0\)|0)\)\n' .; then
    echo ""
    echo "Use require.Zero instead of require.Equal when testing for 0."
    echo ""
    return 1
  fi
}

function test_require_len_zero {
  if grep -R -o -P 'require\.Len\((t, )?.+, 0(,|\))' .; then
    echo ""
    echo "Use require.Empty instead of require.Len when testing for 0 length."
    echo ""
    return 1
  fi
}

function test_require_equal_len {
  # This should only flag if len(foo) is the *actual* val, not the expected val.
  #
  # These should *not* match:
  # - require.Equal(len(foo), 2)
  # - require.Equal(t, len(foo), 2)
  #
  # These should match:
  # - require.Equal(2, len(foo))
  # - require.Equal(t, 2, len(foo))
  if grep -R -o -P --exclude-dir='scripts' 'require\.Equal\((t, )?.*, len\([^,]*$' .; then
    echo ""
    echo "Use require.Len instead of require.Equal when testing for length."
    echo ""
    return 1
  fi
}

function test_require_nil {
  if grep -R -o -P 'require\..+?!= nil' .; then
    echo ""
    echo "Use require.NotNil when testing for nil inequality."
    echo ""
    return 1
  fi

  if grep -R -o -P 'require\..+?== nil' .; then
    echo ""
    echo "Use require.Nil when testing for nil equality."
    echo ""
    return 1
  fi

  if grep -R -o -P 'require\.ErrorIs.+?nil\)' .; then
    echo ""
    echo "Use require.NoError instead of require.ErrorIs when testing for nil error."
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
