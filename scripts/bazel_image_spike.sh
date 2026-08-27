#!/usr/bin/env bash

set -euo pipefail

# Build the locked Debian builder image, then cross-compile AvalancheGo for both
# supported Linux targets inside it. This Phase 1 spike intentionally does not
# build a runtime OCI image.

avalanchego_path="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache_root="${BAZEL_IMAGE_CACHE_ROOT:-${avalanchego_path}/.cache/bazel-image}"
builder_tag="avalanchego-bazel-builder:phase1"

outer_repository_cache="${cache_root}/outer-repository"
outer_disk_cache="${cache_root}/outer-disk"
inner_repository_cache="${cache_root}/inner-repository"

mkdir -p \
  "$outer_repository_cache" \
  "$outer_disk_cache" \
  "$inner_repository_cache" \
  "${cache_root}/inner-disk-amd64" \
  "${cache_root}/inner-disk-arm64" \
  "${cache_root}/inner-output-amd64" \
  "${cache_root}/inner-output-arm64" \
  "${cache_root}/home"

cd "$avalanchego_path"

bazelisk run //bazel/image:load_builder \
  --repository_cache="$outer_repository_cache" \
  --disk_cache="$outer_disk_cache"

builder_digest="$(docker image inspect --format '{{.Id}}' "$builder_tag")"
git_commit="$(git rev-parse HEAD)"
workspace_status="${cache_root}/workspace_status.sh"
printf '#!/bin/sh\nprintf "STABLE_GIT_COMMIT %s\\n"\n' "$git_commit" > "$workspace_status"
chmod 0755 "$workspace_status"

for architecture in amd64 arm64; do
  if [[ "$architecture" == "amd64" ]]; then
    cc=gcc
  else
    cc=aarch64-linux-gnu-gcc
  fi

  docker run --rm \
    --user "$(id -u):$(id -g)" \
    --workdir /workspace \
    --env HOME=/cache/home \
    --env USER=avalanchego \
    --env CC="$cc" \
    --env AVALANCHEGO_BUILDER_IMAGE_DIGEST="$builder_digest" \
    --mount "type=bind,src=$avalanchego_path,dst=/workspace,readonly" \
    --mount "type=bind,src=$inner_repository_cache,dst=/cache/repository" \
    --mount "type=bind,src=${cache_root}/inner-disk-${architecture},dst=/cache/disk" \
    --mount "type=bind,src=${cache_root}/inner-output-${architecture},dst=/cache/output" \
    --mount "type=bind,src=${cache_root}/home,dst=/cache/home" \
    --mount "type=bind,src=$workspace_status,dst=/cache/workspace_status.sh,readonly" \
    "$builder_tag" \
    /usr/local/bin/bazel \
      --output_user_root=/cache/output \
      build \
      --symlink_prefix=/cache/output/bazel- \
      --repository_cache=/cache/repository \
      --disk_cache=/cache/disk \
      --lockfile_mode=error \
      --config=release \
      --workspace_status_command=/cache/workspace_status.sh \
      --compilation_mode=opt \
      --platforms="//bazel/image:linux_${architecture}" \
      --toolchain_resolution_debug='@bazel_tools//tools/cpp:toolchain_type' \
      --subcommands \
      --action_env=CC="$cc" \
      --action_env=AVALANCHEGO_BUILDER_IMAGE_DIGEST="$builder_digest" \
      //main:avalanchego \
    2>&1 | tee "${cache_root}/inner-${architecture}.log"

  binary_path="$(find "${cache_root}/inner-output-${architecture}" -type f -path '*/execroot/_main/bazel-out/*/bin/main/main_/main' -print -quit)"
  if [[ -z "$binary_path" ]]; then
    echo "failed to find ${architecture} AvalancheGo output" >&2
    exit 1
  fi

  echo "inspecting ${architecture} output: ${binary_path}"
  readelf --file-header --program-headers "$binary_path"

  echo "running ${architecture} output in fresh Debian 12 slim"
  docker run --rm \
    --platform "linux/${architecture}" \
    --mount "type=bind,src=${binary_path},dst=/avalanchego,readonly" \
    debian:12-slim \
    /avalanchego --version
done
