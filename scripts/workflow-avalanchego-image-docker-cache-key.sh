#!/usr/bin/env bash

set -euo pipefail

: "${GITHUB_OUTPUT:?GITHUB_OUTPUT must be set}"
: "${RUNNER_OS:?RUNNER_OS must be set}"
: "${RUNNER_ARCH:?RUNNER_ARCH must be set}"

# This archive contains only the external registry and Debian images selected
# by bazel_image_spike.sh. Bazel repositories and Bazel-built images use their
# own caches and do not affect this key.
inputs=(
  scripts/bazel_image_spike.sh
)
input_digest="$(sha256sum "${inputs[@]}" | sha256sum | awk '{print $1}')"
printf 'key=avalanchego-image-docker-v2-%s-%s-%s\n' \
  "$RUNNER_OS" "$RUNNER_ARCH" "$input_digest" >> "$GITHUB_OUTPUT"
