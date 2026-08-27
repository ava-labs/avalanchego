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
  scripts/build_image_bazel.sh
)
input_digest="$(sha256sum "${inputs[@]}" | sha256sum | awk '{print $1}')"
prefix="avalanchego-image-docker-v1-${RUNNER_OS}-${RUNNER_ARCH}-"
# Image repositories depend on the Go graph and configured image transitions,
# not just the Docker archive inputs. Keep this entry separate from both the
# archive and the ordinary setup-bazel dependency cache.
dependency_inputs=(
  "${inputs[@]}"
  .bazelrc
  go.work go.work.sum
  go.mod go.sum
  graft/coreth/go.mod graft/coreth/go.sum
  graft/evm/go.mod graft/evm/go.sum
  graft/subnet-evm/go.mod graft/subnet-evm/go.sum
  scripts/workflow-avalanchego-image-docker-cache-key.sh
  .github/actions/setup-bazel-image-inputs/action.yml
)
# A BUILD change can add a dependency to the image graph without changing
# go.mod (for example, a module previously used only by tests).
while IFS= read -r -d '' build_input; do
  dependency_inputs+=("$build_input")
done < <(find . -type d \( -name .git -o -name .cache -o -name .direnv \) -prune -o \
  -type f \( -name BUILD -o -name BUILD.bazel -o -name '*.bzl' \) -print0 | sort -z)
dependency_digest="$(sha256sum "${dependency_inputs[@]}" | sha256sum | awk '{print $1}')"
dependency_prefix="avalanchego-image-deps-v1-${RUNNER_OS}-${RUNNER_ARCH}-"
{
  echo "prefix=$prefix"
  echo "key=${prefix}${input_digest}"
  echo "dependency-prefix=$dependency_prefix"
  echo "dependency-key=${dependency_prefix}${dependency_digest}"
} >> "$GITHUB_OUTPUT"
