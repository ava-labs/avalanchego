#!/usr/bin/env bash

set -euo pipefail

# Run polyrepo tool for managing local dependencies.
#
# Usage:
#   ./scripts/run_polyrepo.sh [polyrepo args...]
#
# Environment variables (optional):
#   LIBEVM_REF   - Git ref for libevm (e.g., v1.2.3 or commit hash)
#                  Runs: go get github.com/ava-labs/libevm@<ref> && go mod tidy
#   FIREWOOD_REF - Git ref for firewood (e.g., commit hash)
#                  Runs: polyrepo sync firewood@<ref>
#
# Examples:
#   # Direct polyrepo commands
#   ./scripts/run_polyrepo.sh sync firewood@abc123
#   ./scripts/run_polyrepo.sh status
#
#   # Using env vars
#   LIBEVM_REF=v1.2.3 FIREWOOD_REF=abc123 ./scripts/run_polyrepo.sh sync avalanchego@abc123

POLYREPO_REVISION=6239973c9b

if [[ -n "${LIBEVM_REF:-}" ]]; then
  echo "Updating libevm to ${LIBEVM_REF}..."
  go get "github.com/ava-labs/libevm@${LIBEVM_REF}"
  go mod tidy
fi

if [[ -n "${FIREWOOD_REF:-}" ]]; then
  echo "Setting up firewood at ${FIREWOOD_REF}..."
  go run github.com/ava-labs/avalanchego/tests/fixture/polyrepo@"${POLYREPO_REVISION}" sync firewood@"${FIREWOOD_REF}"
fi

[[ $# -gt 0 ]] && go run github.com/ava-labs/avalanchego/tests/fixture/polyrepo@"${POLYREPO_REVISION}" "${@}"
