#!/usr/bin/env bash

set -euo pipefail

# Run polyrepo tool for managing local dependencies.
#
# Environment variables (optional):
#   LIBEVM_REF   - Git ref for libevm (runs: go get && go mod tidy)
#   FIREWOOD_REF - Git ref for firewood (runs: polyrepo sync firewood@ref)
#
# Examples:
#   # Using env vars
#   LIBEVM_REF=v1.2.3 FIREWOOD_REF=abc123 ./scripts/run_polyrepo.sh
#
#   # Using CLI args (local usage)
#   ./scripts/run_polyrepo.sh status
#   LIBEVM_REF=v1.2.3 ./scripts/run_polyrepo.sh sync firewood@abc123
#   ./scripts/run_polyrepo.sh sync avalanchego@xyz --force

POLYREPO_REVISION=6239973c9b
POLYREPO_CMD="go run github.com/ava-labs/avalanchego/tests/fixture/polyrepo@${POLYREPO_REVISION}"

if [[ -n "${LIBEVM_REF:-}" ]]; then
  echo "Updating libevm to ${LIBEVM_REF}..."
  go get "github.com/ava-labs/libevm@${LIBEVM_REF}"
  go mod tidy
fi

if [[ -n "${FIREWOOD_REF:-}" ]]; then
  echo "Syncing firewood at ${FIREWOOD_REF}..."
  $POLYREPO_CMD sync "firewood@${FIREWOOD_REF}"
fi

if [[ $# -gt 0 ]]; then
  $POLYREPO_CMD "${@}"
fi
