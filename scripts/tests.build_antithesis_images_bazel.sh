#!/usr/bin/env bash

set -euo pipefail

# Build Bazel-owned, uninstrumented native Antithesis images in the pinned
# Debian Bazel builder. Docker only loads the completed images and runs the
# compose smoke test. Instrumented amd64 validation is a separate CI target.

AVALANCHE_PATH=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${AVALANCHE_PATH}"

readonly builder_architecture=arm64
readonly builder_tag=docker.io/library/avalanchego-bazel-builder:phase1
readonly bazel_targets=(
  //bazel/antithesis:load_avalanchego_node
  //bazel/antithesis:load_avalanchego_workload
  //bazel/antithesis:load_avalanchego_config
)

case "$(uname -m)" in
  aarch64 | arm64) ;;
  *)
    echo "local Bazel Antithesis validation requires a native arm64 host" >&2
    exit 1
    ;;
esac

if [[ ! -S /var/run/docker.sock ]]; then
  echo "Docker socket /var/run/docker.sock is required for the Bazel image load" >&2
  exit 1
fi

bazel_cmd=(bazelisk)
if [[ -n "${BAZELISK_OUTPUT_USER_ROOT:-}" ]]; then
  bazel_cmd+=("--output_user_root=${BAZELISK_OUTPUT_USER_ROOT}")
fi

if ! docker image inspect "${builder_tag}" > /dev/null 2>&1; then
  "${bazel_cmd[@]}" run "//bazel/image:load_builder_${builder_architecture}"
fi

cache_root="${AVALANCHE_PATH}/.cache/bazel-antithesis"
mkdir -p "${cache_root}/repository" "${cache_root}/disk" "${cache_root}/output" "${cache_root}/home"

docker_socket_gid="$(stat --format=%g /var/run/docker.sock)"
for target in "${bazel_targets[@]}"; do
  docker run --rm \
    --user "$(id -u):$(id -g)" \
    --group-add "${docker_socket_gid}" \
    --workdir /workspace \
    --env HOME=/cache/home \
    --env USER=avalanchego \
    --env CC=gcc \
    --env IMG_INSECURE=1 \
    --mount "type=bind,src=${AVALANCHE_PATH},dst=/workspace,readonly" \
    --mount "type=bind,src=${cache_root}/repository,dst=/cache/repository" \
    --mount "type=bind,src=${cache_root}/disk,dst=/cache/disk" \
    --mount "type=bind,src=${cache_root}/output,dst=/cache/output" \
    --mount "type=bind,src=${cache_root}/home,dst=/cache/home" \
    --mount "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock" \
    "${builder_tag}" \
    /usr/local/bin/bazel \
      --batch \
      --output_user_root=/cache/output \
      run --config=race "${target}" \
      --repository_cache=/cache/repository \
      --disk_cache=/cache/disk \
      --platforms="//bazel/image:linux_${builder_architecture}" \
      --extra_execution_platforms="//bazel/image:linux_${builder_architecture}" \
      --action_env=CC=gcc
done

for image in \
  antithesis-avalanchego-node:bazel \
  antithesis-avalanchego-workload:bazel \
  antithesis-avalanchego-config:bazel; do
  architecture="$(docker image inspect --format '{{.Architecture}}' "${image}")"
  if [[ "${architecture}" != "${builder_architecture}" ]]; then
    echo "${image} has architecture ${architecture}; expected ${builder_architecture}" >&2
    exit 1
  fi
done

export IMAGE_NAME=antithesis-avalanchego-config
export IMAGE_TAG=bazel
export DEBUG="${DEBUG:-}"
# shellcheck source=/dev/null
source "${AVALANCHE_PATH}/scripts/lib_test_antithesis_images.sh"
