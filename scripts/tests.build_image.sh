#!/usr/bin/env bash

set -euo pipefail

# TODO(marun) Perform more extensive validation (e.g. e2e testing) against one or more images

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

source "$AVALANCHE_PATH"/scripts/constants.sh

build_and_test() {
  local repo=$1

  DOCKER_REPO="${repo}" ./scripts/build_image.sh

  # Check for image sanity by calling the binary with --version
  local images=(
    "$repo:$commit_hash"
    "$repo:$current_branch"
    "$repo:$commit_hash-race"
    "$repo:$current_branch-race"
  )
  for image in "${images[@]}"; do
    echo "running 'avalanchego --version' for image ${image}"
    docker run  -t --rm "${image}" /avalanchego/build/avalanchego --version
  done
}

echo "checking build of local single-arch images"
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

# TODO(marun) Check the sanity of images built for the non-host arch
echo "checking build of multi-arch images"
build_and_test "localhost:${REGISTRY_PORT}/avalanchego"
