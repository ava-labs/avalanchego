#!/usr/bin/env bash

set -euo pipefail

# Sanity check the image build by attempting to build and run the image without error.

# Directory above this script
SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)
# Load the constants
# shellcheck source=graft/subnet-evm/scripts/constants.sh disable=SC1091
source "$SUBNET_EVM_PATH"/scripts/constants.sh

build_and_test() {
  local imagename="${1}"
  local vm_id="${2}"
  local multiarch_image="${3}"
  # The local image name will be used to build a local image if the
  # current avalanchego version lacks a published image.
  local avalanchego_local_image_name="${4}"

  if [[ "${multiarch_image}" == true ]]; then
    local arches="linux/amd64,linux/arm64"
  else
    # Test only the host platform for single arch builds
    local host_arch
    host_arch="$(go env GOARCH)"
    local arches="linux/$host_arch"
  fi

  local imgtag="testtag"

  PLATFORMS="${arches}" \
    BUILD_IMAGE_ID="${imgtag}" \
    VM_ID=$"${vm_id}" \
    IMAGE_NAME="${imagename}" \
    AVALANCHEGO_LOCAL_IMAGE_NAME="${avalanchego_local_image_name}" \
    ./scripts/build_docker_image.sh

  echo "listing images"
  docker images

  # Check all of the images expected to have been built
  local target_images=(
    "$imagename:$imgtag"
    "$imagename:$commit_hash"
  )
  IFS=',' read -r -a archarray <<<"$arches"
  for arch in "${archarray[@]}"; do
    for target_image in "${target_images[@]}"; do
      echo "checking sanity of image $target_image for $arch by running '${VM_ID} version'"
      docker run -t --rm --platform "$arch" "$target_image" /avalanchego/build/plugins/"${VM_ID}" --version
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
