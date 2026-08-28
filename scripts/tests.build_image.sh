#!/usr/bin/env bash

set -euo pipefail

# This test script is intended to execute successfully on a ubuntu 22.04 host with either the
# amd64 or arm64 arches. Recent docker (with buildx support) and qemu are required. See
# build_image.sh for more details.

# TODO(marun) Perform more extensive validation (e.g. e2e testing) against one or more images

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

source "$AVALANCHE_PATH"/scripts/constants.sh
source "$AVALANCHE_PATH"/scripts/git_commit.sh
source "$AVALANCHE_PATH"/scripts/image_tag.sh

IMAGE_RUN_TIMEOUT_SECONDS="${IMAGE_RUN_TIMEOUT_SECONDS:-60}"

run_image() {
  local image_name=$1
  local arch=$2
  local container_id
  local exit_code

  container_id="$(docker create --platform "linux/$arch" "$image_name" /avalanchego/build/avalanchego --version)"
  if ! docker start "$container_id" > /dev/null; then
    docker logs "$container_id" || true
    docker rm -f "$container_id" > /dev/null || true
    return 1
  fi

  if ! exit_code="$(timeout --kill-after=10s "${IMAGE_RUN_TIMEOUT_SECONDS}s" docker wait "$container_id")"; then
    echo "timed out after ${IMAGE_RUN_TIMEOUT_SECONDS}s running $image_name for $arch" >&2
    docker logs "$container_id" || true
    docker rm -f "$container_id" > /dev/null || true
    return 1
  fi

  docker logs "$container_id"
  docker rm "$container_id" > /dev/null
  if [[ "$exit_code" != 0 ]]; then
    echo "$image_name exited with status $exit_code for $arch" >&2
    return 1
  fi
}

build_and_test() {
  local image_name=$1

  BUILDX_BUILDER="$BUILDER_NAME" BUILD_MULTI_ARCH=1 DOCKER_IMAGE="$image_name" ./scripts/build_image.sh

  echo "listing images"
  docker images

  local host_arch
  host_arch="$(go env GOARCH)"

  if [[ "$image_name" == *"/"* ]]; then
    # Test all arches if testing a multi-arch image
    local arches=("amd64" "arm64")
  else
    # Test only the host platform for single arch builds
    local arches=("$host_arch")
  fi

  # Check all of the images expected to have been built
  local target_images=(
    "$image_name:$commit_hash"
    "$image_name:$image_tag"
    "$image_name:$commit_hash-r"
    "$image_name:$image_tag-r"
  )

  for arch in "${arches[@]}"; do
    for target_image in "${target_images[@]}"; do
      if [[ "$host_arch" == "amd64" && "$arch" == "arm64" && "$target_image" =~ "-r" ]]; then
        # Error reported when trying to sanity check this configuration in GitHub CI:
        #
        #   FATAL: ThreadSanitizer: unsupported VMA range
        #   FATAL: Found 39 - Supported 48
        #
        echo "skipping sanity check for $target_image"
        echo "image is for arm64 and binary is compiled with race detection"
        echo "amd64 GitHub workers are known to run kernels incompatible with these images"
      else
        echo "checking sanity of image $target_image for $arch by running 'avalanchego --version'"
        run_image "$target_image" "$arch"
      fi
    done
  done
}

# Use a known builder rather than whichever builder is selected in the caller's
# Docker configuration. A builder with this name may be left by an interrupted
# earlier test run, in which case it is safe to reuse it.
BUILDER_NAME=ci-builder
BUILDER_CREATED=0
REGISTRY_CONTAINER_ID=""

# Ensure registry and a builder created by this invocation are cleaned up.
function cleanup {
  if [[ -n "$REGISTRY_CONTAINER_ID" ]]; then
    echo "stopping local docker registry"
    docker stop "$REGISTRY_CONTAINER_ID"
  fi
  if [[ "$BUILDER_CREATED" -eq 1 ]]; then
    echo "removing multiplatform builder $BUILDER_NAME"
    docker buildx rm "$BUILDER_NAME"
  fi
}
trap cleanup EXIT

if docker buildx inspect "$BUILDER_NAME" > /dev/null 2>&1; then
  echo "reusing existing multiplatform builder $BUILDER_NAME"
else
  echo "creating multiplatform builder $BUILDER_NAME"
  # '--driver-opt network=host' enables the builder to use the local registry.
  docker buildx create --name "$BUILDER_NAME" --driver-opt network=host
  BUILDER_CREATED=1
fi

# Start the builder before a build and report its supported platforms. This
# also verifies that QEMU was registered before the test started.
docker buildx inspect --builder "$BUILDER_NAME" --bootstrap
builder_inspect="$(docker buildx inspect --builder "$BUILDER_NAME")"
if ! grep -Fq 'network="host"' <<<"$builder_inspect"; then
  echo "builder $BUILDER_NAME must use the host network to reach the local registry" >&2
  exit 1
fi

echo "checking build of single-arch images"
build_and_test avalanchego

echo "starting local docker registry to allow verification of multi-arch image builds"
REGISTRY_CONTAINER_ID="$(docker run --rm -d -P registry:2)"
REGISTRY_PORT="$(docker port "$REGISTRY_CONTAINER_ID" 5000/tcp | grep -v "::" | awk -F: '{print $NF}')"

echo "checking build of multi-arch images"
build_and_test "localhost:${REGISTRY_PORT}/avalanchego"
