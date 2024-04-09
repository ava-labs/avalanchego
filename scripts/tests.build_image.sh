#!/usr/bin/env bash

set -euo pipefail

# TODO(marun) Perform more extensive validation (e.g. e2e testing) against one or more images

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

source "$AVALANCHE_PATH"/scripts/constants.sh

build_and_test() {
  local repo=$1

  DOCKER_REPO="$repo" ./scripts/build_image.sh

  echo "listing images"
  docker images

  local host_arch
  host_arch="$(go env GOARCH)"

  if [[ "$repo" == *"/"* ]]; then
    # Test all arches if testing a multi-arch image
    local arches=("amd64" "arm64")
  else
    # Test only the host platform for single arch builds
    local arches=("$host_arch")
  fi

  # Check all of the images expected to have been built
  local images=(
    "$repo:$commit_hash"
    "$repo:$current_branch"
    "$repo:$commit_hash-race"
    "$repo:$current_branch-race"
  )

  for arch in "${arches[@]}"; do
    for image in "${images[@]}"; do
      if [[ "$host_arch" == "amd64" && "$arch" == "arm64" && "$image" =~ "-race" ]]; then
        # Error reported when trying to sanity check this configuration:
        #
        #   FATAL: ThreadSanitizer: unsupported VMA range
        #   FATAL: Found 39 - Supported 48
        #
        echo "skipping sanity check for $image"
        echo "image is for arm64 and binary is compiled with race detection"
        echo "amd64 github workers are known to run kernels incompatible with these images"
      else
        echo "checking sanity of image $image for $arch by running 'avalanchego --version'"
        docker run  -t --rm --platform "linux/$arch" "$image" /avalanchego/build/avalanchego --version
      fi
    done
  done
}

echo "checking build of single-arch images"
build_and_test avalanchego

echo "starting local docker registry to allow verification of multi-arch image builds"
REGISTRY_CONTAINER_ID="$(docker run --rm -d -P registry:2)"
REGISTRY_PORT="$(docker port "$REGISTRY_CONTAINER_ID" 5000/tcp | grep -v "::" | awk -F: '{print $NF}')"

echo "starting docker builder that supports multiplatform builds"
# - creating a new builder enables multiplatform builds
# - '--driver-opt network=host' enables the builder to use the local registry
docker buildx create --use --name ci-builder --driver-opt network=host

# Ensure registry and builder cleanup on teardown
function cleanup {
  echo "stopping local docker registry"
  docker stop "${REGISTRY_CONTAINER_ID}"
  echo "removing multiplatform builder"
  docker buildx rm ci-builder
}
trap cleanup EXIT

echo "checking build of multi-arch images"
build_and_test "localhost:${REGISTRY_PORT}/avalanchego"
