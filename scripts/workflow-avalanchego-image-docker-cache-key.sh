#!/usr/bin/env bash

set -euo pipefail

: "${GITHUB_OUTPUT:?GITHUB_OUTPUT must be set}"
: "${RUNNER_OS:?RUNNER_OS must be set}"
: "${RUNNER_ARCH:?RUNNER_ARCH must be set}"

inputs=(
  .bazelversion
  MODULE.bazel
  MODULE.bazel.lock
  bazel/image/BUILD.bazel
  bazel/image/bookworm.lock.json
  scripts/bazel_image_spike.sh
)
input_digest="$(sha256sum "${inputs[@]}" | sha256sum | awk '{print $1}')"
printf 'key=avalanchego-image-docker-v1-%s-%s-%s\n' \
  "$RUNNER_OS" "$RUNNER_ARCH" "$input_digest" >> "$GITHUB_OUTPUT"
