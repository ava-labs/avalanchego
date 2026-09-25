#!/usr/bin/env bash

set -euo pipefail

if (( $# < 4 )); then
  echo "usage: $0 <avalanchego> <e2e-test> <ginkgo> <xsvm> [e2e-test-args...]" >&2
  exit 1
fi

avalanchego_path="$(realpath "$1")"
e2e_test_path="$(realpath "$2")"
ginkgo_path="$(realpath "$3")"
xsvm_path="$(realpath "$4")"
shift 4

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
