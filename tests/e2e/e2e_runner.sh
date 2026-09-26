#!/usr/bin/env bash

set -euo pipefail

if [[ "${1:-}" == "--use-staged-binaries" ]]; then
  # CI restores these files from the artifacts produced by the staging tasks.
  staged_binary_dir="build/bazel-e2e"
  avalanchego_path="$(realpath "${staged_binary_dir}/avalanchego/avalanchego")"
  e2e_test_path="$(realpath "${staged_binary_dir}/runtime/e2e.test")"
  ginkgo_path="$(realpath "${staged_binary_dir}/runtime/ginkgo")"
  xsvm_path="$(realpath "${staged_binary_dir}/runtime/xsvm")"
  # actions/download-artifact stores files in a ZIP archive, which does not
  # preserve executable permissions. The staged directory is workspace-owned.
  chmod +x "${avalanchego_path}" "${e2e_test_path}" "${ginkgo_path}" "${xsvm_path}"
  shift
else
  if (( $# < 4 )); then
    echo "usage: $0 [--use-staged-binaries] [e2e-test-args...]" >&2
    exit 1
  fi

  avalanchego_path="$(realpath "$1")"
  e2e_test_path="$(realpath "$2")"
  ginkgo_path="$(realpath "$3")"
  xsvm_path="$(realpath "$4")"
  shift 4
fi

# Keep tmpnet data in the user's home directory so it remains easy to inspect.
# The plugin directory is separate from the network data and contains a symlink
# to the Bazel-built XSVM binary.
plugin_dir="${TMPNET_BAZEL_PLUGIN_DIR:-${HOME}/.tmpnet/plugins/bazel-e2e}"
mkdir -p "${plugin_dir}"
ln -sfn "${xsvm_path}" "${plugin_dir}/v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH"
export AVAGO_PLUGIN_DIR="${plugin_dir}"

ginkgo_args=(-v)
if [[ -n "${E2E_SERIAL:-}" ]]; then
  echo "tests will be executed serially to minimize resource requirements"
else
  echo "tests will be executed in parallel"
  ginkgo_args+=(-p)
fi

if [[ -n "${E2E_RANDOM_SEED:-}" ]]; then
  ginkgo_args+=("--seed=${E2E_RANDOM_SEED}")
else
  ginkgo_args+=(--randomize-all)
fi

exec "${ginkgo_path}" "${ginkgo_args[@]}" "${e2e_test_path}" -- \
  "--avalanchego-path=${avalanchego_path}" \
  "$@"
