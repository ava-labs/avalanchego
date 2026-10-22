#!/usr/bin/env bash

set -euo pipefail

# Run the prebuilt E2E suite with Bazel-built runtime dependencies.
# Invoke the Bazel target with `task bazel-test-e2e` from the repository root.
# Bazel supplies four binary paths before any test arguments.
# Forward remaining arguments to the test binary, including --ginkgo.* flags.

if (( $# < 4 )); then
  echo "usage: $0 <avalanchego> <e2e.test> <ginkgo> <xsvm> [e2e-test-args...]" >&2
  exit 1
fi

# Resolve Bazel runfile paths before Ginkgo changes the working directory.
AVALANCHEGO_PATH="$(realpath "$1")"
E2E_TEST_PATH="$(realpath "$2")"
GINKGO_PATH="$(realpath "$3")"
XSVM_PATH="$(realpath "$4")"
shift 4

# Keep plugins in the workspace, separate from plugins from non-Bazel builds.
# Bazel runs from its runfiles directory, so use BUILD_WORKSPACE_DIRECTORY.
# AvalancheGo finds XSVM by its VM ID, which is the symlink name below.
REPO_ROOT="${BUILD_WORKSPACE_DIRECTORY:-${PWD}}"
PLUGIN_DIR="${REPO_ROOT}/build/bazel-e2e/plugins"
mkdir -p "${PLUGIN_DIR}"
ln -sfn "${XSVM_PATH}" "${PLUGIN_DIR}/v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH"
export AVAGO_PLUGIN_DIR="${PLUGIN_DIR}"

# Use the shared runner for test options and argument forwarding.
# All binary paths are absolute, so changing to the workspace root is safe.
# The prebuilt E2E target runs without invoking a Go compiler.
E2E_TEST_SCRIPT="${E2E_TEST_SCRIPT:-./scripts/tests.e2e.sh}"
if [[ "${1:-}" == "--existing-network" ]]; then
  E2E_TEST_SCRIPT=./scripts/tests.e2e.existing.sh
  shift
fi
export GINKGO_PATH AVALANCHEGO_PATH
export E2E_TARGET="${E2E_TEST_PATH}"
cd "${REPO_ROOT}"
exec "${E2E_TEST_SCRIPT}" "$@"
