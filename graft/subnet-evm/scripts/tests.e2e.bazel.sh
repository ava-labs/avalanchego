#!/usr/bin/env bash

set -euo pipefail

# Run a Bazel-built Subnet-EVM E2E test with Bazel-built runtime binaries.
if (( $# < 3 )); then
  echo "usage: $0 <avalanchego> <subnet-evm> <test> [--simulator <path>] [test-args...]" >&2
  exit 1
fi

REPO_ROOT="${BUILD_WORKSPACE_DIRECTORY:-${PWD}}"
AVALANCHEGO_PATH="$(realpath "$1")"
AVAGO_PLUGIN_DIR="${REPO_ROOT}/build/bazel-e2e/plugins"
PLUGIN_PATH="$(realpath "$2")"
TEST_PATH="$(realpath "$3")"
export AVALANCHEGO_PATH AVAGO_PLUGIN_DIR
shift 3

if [[ "${1:-}" == "--simulator" ]]; then
  SIMULATOR_PATH="$(realpath "${2:?--simulator requires a path}")"
  export SIMULATOR_PATH
  shift 2
fi

# Load only VM identity; constants.sh also runs Go and configures builds.
# shellcheck disable=SC1091
source "${REPO_ROOT}/graft/subnet-evm/scripts/vm_constants.sh"

mkdir -p "${AVAGO_PLUGIN_DIR}"
ln -sfn "${PLUGIN_PATH}" "${AVAGO_PLUGIN_DIR}/${DEFAULT_VM_ID}"

cd "${REPO_ROOT}/graft/subnet-evm"
exec "${TEST_PATH}" --ginkgo.vv --ginkgo.label-filter="${GINKGO_LABEL_FILTER:-}" "$@"
