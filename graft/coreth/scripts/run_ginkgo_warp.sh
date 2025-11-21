#!/usr/bin/env bash

set -euo pipefail

CORETH_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

source "$CORETH_PATH"/scripts/constants.sh

EXTRA_ARGS=()
AVALANCHEGO_BUILD_PATH="${AVALANCHEGO_BUILD_PATH:-}"
if [[ -n "${AVALANCHEGO_BUILD_PATH}" ]]; then
  EXTRA_ARGS=("--avalanchego-path=${AVALANCHEGO_BUILD_PATH}/avalanchego")
  echo "Running with extra args:" "${EXTRA_ARGS[@]}"
fi

"${CORETH_PATH}"/bin/ginkgo -vv --label-filter="${GINKGO_LABEL_FILTER:-}" "${CORETH_PATH}"/tests/warp -- "${EXTRA_ARGS[@]}"
