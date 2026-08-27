#!/usr/bin/env bash

# Integration test: use real Docker and Bazel, not command substitutes.
# This test downloads inputs and builds both architectures. Its temporary files
# stay inside the repository so Docker can mount them in the native builder.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
mkdir -p .cache/bazel-image
root="$(mktemp -d "$repo_root/.cache/bazel-image/test-inputs.XXXXXX")"
cleanup() {
  local status=$?
  if [[ "$status" != 0 ]]; then
    echo "Test inputs retained at $root" >&2
    return "$status"
  fi
  # Bazel creates read-only output directories. These are all test-owned files.
  chmod -R u+w "$root"
  rm -rf "$root"
}
trap cleanup EXIT

export BAZEL_IMAGE_CACHE_ROOT="$root/prepared"
export BAZEL_IMAGE_DOCKER_CACHE_ARCHIVE="$root/docker-images.tar"
export BAZEL_REMOTE_CACHE_ENABLED=false

# A cold preparation must produce reusable dependency caches and an archive.
BAZEL_IMAGE_WRITE_DOCKER_CACHE=true ./scripts/build_image_bazel.sh --prepare-inputs
[[ -s "$BAZEL_IMAGE_DOCKER_CACHE_ARCHIVE" ]]
[[ -d "$BAZEL_IMAGE_CACHE_ROOT/inner-repository" ]]
[[ -d "$BAZEL_IMAGE_CACHE_ROOT/inner-go-modules/cache/download" ]]

# An exact hit must load prepared inputs without permission to download them.
BAZEL_IMAGE_PREPARE_DOCKER=false BAZEL_IMAGE_PREPARE_DEPENDENCIES=false \
  ./scripts/build_image_bazel.sh --prepare-inputs

# Emulate a new job: preserve only the saved input caches, not Bazel's output
# root, HOME, disk cache, or extracted external repositories.
export BAZEL_IMAGE_CACHE_ROOT="$root/validation"
mkdir -p "$BAZEL_IMAGE_CACHE_ROOT"
cp -a "$root/prepared/inner-repository" "$root/prepared/inner-go-modules" \
  "$root/prepared/outer-repository" "$BAZEL_IMAGE_CACHE_ROOT/"
BAZEL_IMAGE_DISABLE_DOWNLOADS=true ./scripts/build_image_bazel.sh --build-only

# Missing dependency inputs must fail rather than trigger a download.
export BAZEL_IMAGE_CACHE_ROOT="$root/missing"
mkdir -p "$BAZEL_IMAGE_CACHE_ROOT"
cp -a "$root/prepared/outer-repository" "$BAZEL_IMAGE_CACHE_ROOT/"
if BAZEL_IMAGE_DISABLE_DOWNLOADS=true ./scripts/build_image_bazel.sh --build-only > "$root/missing.log" 2>&1; then
  echo 'image build unexpectedly succeeded with empty inner input caches' >&2
  exit 1
fi
grep -Eq 'download.*disabled|GOPROXY=off|module lookup disabled' "$root/missing.log"

# An exact-hit preparation must also reject a missing outer repository cache,
# even outside CI where downloads are normally allowed for local builds.
export BAZEL_IMAGE_CACHE_ROOT="$root/missing-outer"
if BAZEL_IMAGE_PREPARE_DOCKER=false BAZEL_IMAGE_PREPARE_DEPENDENCIES=false \
  ./scripts/build_image_bazel.sh --prepare-inputs > "$root/missing-outer.log" 2>&1; then
  echo 'exact-hit preparation unexpectedly downloaded missing outer inputs' >&2
  exit 1
fi
grep -Eq 'download.*disabled|GOPROXY=off|module lookup disabled' "$root/missing-outer.log"
echo 'Bazel image input integration tests passed'
