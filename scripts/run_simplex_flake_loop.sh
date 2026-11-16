#!/usr/bin/env bash

# Rerun the simplex Go tests exactly as the scheduled race-shuffle unit job does
# (scripts/tests.unit.sh, restricted to graft/simplex/...) until a run fails or
# the time budget is spent.
#
# e.g.,
# ./scripts/run_simplex_flake_loop.sh
# SIMPLEX_LOOP_BUDGET_SECONDS=600 ./scripts/run_simplex_flake_loop.sh

set -euo pipefail

budget_seconds="${SIMPLEX_LOOP_BUDGET_SECONDS:-19200}"
deadline=$((SECONDS + budget_seconds))
iteration=0

export UNIT_TEST_PACKAGE_PREFIX="github.com/ava-labs/avalanchego/graft/simplex"

while ((SECONDS < deadline)); do
  iteration=$((iteration + 1))
  echo "=== simplex flake loop: iteration ${iteration} ($((deadline - SECONDS))s of budget left) ==="
  # -count=1 forces every iteration to execute the tests rather than reuse cached results.
  if ! bash "$(dirname "${BASH_SOURCE[0]}")/tests.unit.sh" -count=1; then
    echo "=== simplex flake loop: failure on iteration ${iteration} ===" >&2
    exit 1
  fi
done

echo "=== simplex flake loop: ${iteration} iterations passed within the time budget ==="
