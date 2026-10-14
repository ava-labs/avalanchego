#!/usr/bin/env bash

set -euo pipefail

# Run a Bazel-built Subnet-EVM E2E test with Bazel-built runtime binaries.
if (( $# < 4 )); then
  echo "usage: $0 <avalanchego> <subnet-evm> <test> <simulator> [test-args...]" >&2
  exit 1
fi

REPO_ROOT="${BUILD_WORKSPACE_DIRECTORY:-${PWD}}"
AVALANCHEGO_PATH="$(realpath "$1")"
AVALANCHEGO_BUILD_PATH="$(dirname "${AVALANCHEGO_PATH}")"
AVALANCHEGO_PLUGIN_DIR="${REPO_ROOT}/build/bazel-e2e/plugins"
AVAGO_PLUGIN_DIR="${AVALANCHEGO_PLUGIN_DIR}"
PLUGIN_PATH="$(realpath "$2")"
TEST_PATH="$(realpath "$3")"
SIMULATOR_PATH="$(realpath "$4")"
export AVALANCHEGO_PATH AVALANCHEGO_BUILD_PATH AVALANCHEGO_PLUGIN_DIR AVAGO_PLUGIN_DIR SIMULATOR_PATH
shift 4

mkdir -p "${AVALANCHEGO_PLUGIN_DIR}"
ln -sfn "${PLUGIN_PATH}" "${AVALANCHEGO_PLUGIN_DIR}/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"

cd "${REPO_ROOT}/graft/subnet-evm"
exec "${TEST_PATH}" --ginkgo.vv "$@"
