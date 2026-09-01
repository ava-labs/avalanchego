#!/usr/bin/env bash

set -euo pipefail

# Verify the package-discovery and package-filtering contract of tests.unit.sh.
# Keep this test aligned with intentional changes to its supported-package policy.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bash_bin="$(command -v bash)"

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

stub_dir="${workdir}/bin"
mkdir -p "${stub_dir}"

# Replace go with a deterministic stub. This isolates package selection from Go
# package execution and permits an otherwise hard-to-produce discovery failure.
cat >"${stub_dir}/go" <<EOF
#!${bash_bin}
set -euo pipefail

case "\${1-}" in
  list)
    # The failure fixture verifies that errors from each module's discovery are
    # returned before tests are run.
    if [[ -n "\${FAIL_CORETH_DISCOVERY-}" && "\${PWD}" == "${repo_root}/graft/coreth" ]]; then
      exit 42
    fi
    # These fixtures include both selected and excluded packages so the expected
    # go test arguments below protect the filtering policy.
    case "\${PWD}" in
      "${repo_root}")
        printf '%s\n' \
          github.com/ava-labs/avalanchego/unit \
          github.com/ava-labs/avalanchego/mocks \
          github.com/ava-labs/avalanchego/proto/pb \
          github.com/ava-labs/avalanchego/tests/antithesis \
          github.com/ava-labs/avalanchego/tests/e2e
        ;;
      "${repo_root}/graft/coreth")
        printf '%s\n' \
          github.com/ava-labs/avalanchego/graft/coreth/unit \
          github.com/ava-labs/avalanchego/graft/coreth/mocks \
          github.com/ava-labs/avalanchego/graft/coreth/tests/warp
        ;;
      "${repo_root}/graft/evm")
        printf '%s\n' \
          github.com/ava-labs/avalanchego/graft/evm/unit \
          github.com/ava-labs/avalanchego/graft/evm/tests/e2e
        ;;
      "${repo_root}/graft/subnet-evm")
        printf '%s\n' \
          github.com/ava-labs/avalanchego/graft/subnet-evm/unit \
          github.com/ava-labs/avalanchego/graft/subnet-evm/proto/pb \
          github.com/ava-labs/avalanchego/graft/subnet-evm/tests/warp
        ;;
      *)
        printf 'unexpected package-list directory: %s\n' "\${PWD}" >&2
        exit 98
        ;;
    esac
    ;;
  test)
    printf '%s\n' "\${@:2}" >"${workdir}/test-args"
    ;;
  *)
    printf 'unexpected go command: %s\n' "\$*" >&2
    exit 99
    ;;
esac
EOF
chmod +x "${stub_dir}/go"

# A discovery failure must stop the script and prevent a partial test run.
set +e
PATH="${stub_dir}:${PATH}" FAIL_CORETH_DISCOVERY=1 NO_RACE=1 NO_SHUFFLE=1 \
  "${repo_root}/scripts/tests.unit.sh"
status=$?
set -e

if [[ ${status} -ne 42 ]]; then
  echo "expected package discovery to exit with status 42, got ${status}" >&2
  exit 1
fi
if [[ -e "${workdir}/test-args" ]]; then
  echo "go test ran after package discovery failed" >&2
  exit 1
fi

# A successful discovery must invoke go test with only supported packages and
# the stable unit-test flags. Update this expectation only with an intentional
# change to tests.unit.sh's package-selection contract.
PATH="${stub_dir}:${PATH}" NO_RACE=1 NO_SHUFFLE=1 \
  "${repo_root}/scripts/tests.unit.sh"

cat >"${workdir}/expected-test-args" <<'EOF'
-timeout=900s
-coverprofile=coverage-all.out
-covermode=atomic
github.com/ava-labs/avalanchego/unit
github.com/ava-labs/avalanchego/tests/antithesis
github.com/ava-labs/avalanchego/graft/coreth/unit
github.com/ava-labs/avalanchego/graft/coreth/mocks
github.com/ava-labs/avalanchego/graft/evm/unit
github.com/ava-labs/avalanchego/graft/evm/tests/e2e
github.com/ava-labs/avalanchego/graft/subnet-evm/unit
github.com/ava-labs/avalanchego/graft/subnet-evm/proto/pb
EOF

if ! diff -u "${workdir}/expected-test-args" "${workdir}/test-args"; then
  echo "unit-test package selection or arguments did not match" >&2
  exit 1
fi

echo "unit-test script tests passed"
