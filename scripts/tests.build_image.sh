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
source "$AVALANCHE_PATH"/scripts/lib_test_docker_image.sh

build_and_test() {
  local image_name=$1

  BUILD_MULTI_ARCH=1 DOCKER_IMAGE="$image_name" ./scripts/build_image.sh

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
        # Error reported when trying to sanity check this configuration in github ci:
        #
        #   FATAL: ThreadSanitizer: unsupported VMA range
        #   FATAL: Found 39 - Supported 48
        #
        echo "skipping sanity check for $target_image"
        echo "image is for arm64 and binary is compiled with race detection"
        echo "amd64 github workers are known to run kernels incompatible with these images"
      else
        echo "checking sanity of image $target_image for $arch by running 'avalanchego --version'"
        docker run  -t --rm --platform "linux/$arch" "$target_image" /avalanchego/build/avalanchego --version
      fi
    done
  done
}

echo "checking build of single-arch images"
build_and_test avalanchego

start_test_registry

echo "checking build of multi-arch images"
build_and_test "localhost:${REGISTRY_PORT}/avalanchego"
