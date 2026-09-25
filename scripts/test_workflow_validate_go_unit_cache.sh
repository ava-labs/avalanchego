#!/usr/bin/env bash

set -euo pipefail

# Check that every Go test package reports (cached) after an exact GOCACHE
# restore.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

run_check() {
  local name="$1"
  local expected_status="$2"
  local log_path="${workdir}/${name}.log"

  cat >"${log_path}"

  set +e
  "${repo_root}/scripts/workflow-validate-go-unit-cache.sh" "${log_path}"
  local status=$?
  set -e

  if [[ ${status} -ne ${expected_status} ]]; then
    echo "${name}: expected status ${expected_status}, got ${status}" >&2
    exit 1
  fi
}

run_check cached-results-without-coverage 0 <<'EOF'
?   	github.com/ava-labs/avalanchego/utils/units	[no test files]
ok  	github.com/ava-labs/avalanchego/ids	(cached)
EOF

run_check cached-results-with-coverage 0 <<'EOF'
ok  	github.com/ava-labs/avalanchego/ids	(cached)	coverage: 99.0% of statements
EOF

run_check uncached-result 1 <<'EOF'
ok  	github.com/ava-labs/avalanchego/ids	0.003s
EOF

run_check no-results 1 <<'EOF'
?   	github.com/ava-labs/avalanchego/utils/units	[no test files]
EOF

echo "Go unit-cache validation tests passed"
