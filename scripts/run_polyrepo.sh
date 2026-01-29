#!/usr/bin/env bash

set -euo pipefail

# Run polyrepo tool for managing local dependencies.
#
# Environment variables (optional):
#   LIBEVM_REF   - Git ref for libevm (runs: go get && go mod tidy)
#   FIREWOOD_REF - Git ref for firewood (runs: polyrepo sync firewood@ref)
#   FORCE_SYNC   - Set to 1 or true to force sync even if directory exists
#
# Examples:
#   # Using env vars
#   LIBEVM_REF=v1.2.3 FIREWOOD_REF=abc123 ./scripts/run_polyrepo.sh
#   FIREWOOD_REF=abc123 FORCE_SYNC=1 ./scripts/run_polyrepo.sh
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

FORCE_FLAG=""
if [[ "${FORCE_SYNC:-}" == "1" || "${FORCE_SYNC:-}" == "true" ]]; then
  FORCE_FLAG="--force"
fi

if [[ -n "${FIREWOOD_REF:-}" ]]; then
  echo "Syncing firewood at ${FIREWOOD_REF}${FORCE_FLAG:+ (force)}..."
  $POLYREPO_CMD sync "firewood@${FIREWOOD_REF}" $FORCE_FLAG
fi

if [[ $# -gt 0 ]]; then
  $POLYREPO_CMD "${@}"
fi
