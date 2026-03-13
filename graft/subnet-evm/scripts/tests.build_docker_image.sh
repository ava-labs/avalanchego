#!/usr/bin/env bash

set -euo pipefail

# Sanity check the image build by attempting to build and run the image without error.

# Directory above this script
SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)
AVALANCHE_PATH=$(cd "$SUBNET_EVM_PATH" && cd ../.. && pwd)

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

build_and_test() {
  local imagename="${1}"
  local vm_id="${2}"
  local multiarch_image="${3}"
  # The local image name will be used to build a local avalanchego image.
  local avalanchego_local_image_name="${4}"

  if [[ "${multiarch_image}" == true ]]; then
    local arches="linux/amd64,linux/arm64"
  else
    # Test only the host platform for single arch builds
    local host_arch
    host_arch="$(go env GOARCH)"
    local arches="linux/$host_arch"
  fi

  local build_multi_arch=""
  if [[ "$arches" == *,* ]]; then
    build_multi_arch=1
  fi

  # Build avalanchego base image first
  SKIP_BUILD_RACE=1 \
    DOCKER_IMAGE="${avalanchego_local_image_name}" \
    BUILD_MULTI_ARCH="${build_multi_arch}" \
    PLATFORMS="${arches}" \
    "${AVALANCHE_PATH}"/scripts/build_image.sh

  # Build subnet-evm image on top
  # shellcheck disable=SC2154
  TARGET=subnet-evm \
    DOCKER_IMAGE="${imagename}" \
    VM_ID="${vm_id}" \
    AVALANCHEGO_NODE_IMAGE="${avalanchego_local_image_name}:${image_tag}" \
    BUILD_MULTI_ARCH="${build_multi_arch}" \
    PLATFORMS="${arches}" \
    "${AVALANCHE_PATH}"/scripts/build_image.sh

  echo "listing images"
  docker images

  # Check all of the images expected to have been built
  # shellcheck disable=SC2154
  local target_images=(
    "$imagename:$image_tag"
    "$imagename:$commit_hash"
  )
  IFS=',' read -r -a archarray <<<"$arches"
  for arch in "${archarray[@]}"; do
    for target_image in "${target_images[@]}"; do
      echo "checking sanity of image $target_image for $arch by running '${vm_id} version'"
      docker run -t --rm --platform "$arch" "$target_image" /avalanchego/build/plugins/"${vm_id}" --version
    done
  done
}

VM_ID="${VM_ID:-${DEFAULT_VM_ID}}"

echo "checking build of single-arch image"
build_and_test "subnet-evm_avalanchego" "${VM_ID}" false "avalanchego"

echo "starting local docker registry to allow verification of multi-arch image builds"
REGISTRY_CONTAINER_ID="$(docker run --rm -d -P registry:2)"
REGISTRY_PORT="$(docker port "$REGISTRY_CONTAINER_ID" 5000/tcp | grep -v "::" | awk -F: '{print $NF}')"

echo "starting docker builder that supports multiplatform builds"
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
build_and_test "localhost:${REGISTRY_PORT}/subnet-evm_avalanchego" "${VM_ID}" true "localhost:${REGISTRY_PORT}/avalanchego"
