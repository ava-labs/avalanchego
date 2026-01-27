#!/usr/bin/env bash

set -euo pipefail

# If set to non-empty, prompts the building of a multi-arch image when the image
# name indicates use of a registry.
#
# A registry is required to build a multi-arch image since a multi-arch image is
# not really an image at all. A multi-arch image (also called a manifest) is
# basically a list of arch-specific images available from the same registry that
# hosts the manifest. Manifests are not supported for local images.
#
# Reference: https://docs.docker.com/build/building/multi-platform/
PLATFORMS="${PLATFORMS:-}"

# If set to non-empty, the image will be published to the registry.
PUBLISH="${PUBLISH:-}"

# Directory above this script
SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

# Load shared build functions
AVALANCHE_PATH="${SUBNET_EVM_PATH}/../.."
source "$AVALANCHE_PATH"/scripts/lib_build_image.sh

# ALLOW_TAG_LATEST is used to tag the image as 'latest' if set to true.
# It only works if the image is built from the master branch. This is to avoid
# tagging images from a manual triggered build as 'latest' with older avalanchego versions.
ALLOW_TAG_LATEST="${ALLOW_TAG_LATEST:-}"

# buildx (BuildKit) improves the speed and UI of builds over the legacy builder and
# simplifies creation of multi-arch images.
#
# Reference: https://docs.docker.com/build/buildkit/
DOCKER_CMD="docker buildx build"

# Configure build mode (push vs load) and platform flags
if [[ -n "${PUBLISH}" ]]; then
  echo "Pushing $IMAGE_NAME:$BUILD_IMAGE_ID"
fi
configure_docker_build_mode "$IMAGE_NAME" "${PLATFORMS:-}"
DOCKER_CMD="${DOCKER_CMD} ${DOCKER_BUILD_MODE_FLAGS} ${DOCKER_PLATFORM_FLAGS}"

VM_ID=${VM_ID:-"${DEFAULT_VM_ID}"}

# Default to the release image. Will need to be overridden when testing against unreleased versions.
AVALANCHEGO_NODE_IMAGE="${AVALANCHEGO_NODE_IMAGE:-${AVALANCHEGO_IMAGE_NAME}:${AVALANCHE_VERSION}}"

# Build the avalanchego image if it cannot be pulled. This will usually be due to
# AVALANCHE_VERSION being not yet merged since the image is published post-merge.
if ! docker pull "${AVALANCHEGO_NODE_IMAGE}"; then
  # Build a multi-arch avalanchego image if the subnet-evm image build is multi-arch
  BUILD_MULTI_ARCH="$([[ "$PLATFORMS" =~ , ]] && echo 1 || echo "")"

  # - Use a image name without a repository (i.e. without 'avaplatform/' prefix ) to build a
  #   local single-arch image that will not be pushed.
  # - Use a image name with a repository to build a multi-arch image that will be pushed.
  AVALANCHEGO_LOCAL_IMAGE_NAME="${AVALANCHEGO_LOCAL_IMAGE_NAME:-avalanchego}"

  AVALANCHEGO_NODE_IMAGE="${AVALANCHEGO_LOCAL_IMAGE_NAME}:${AVALANCHE_VERSION}"
  echo "Building ${AVALANCHEGO_NODE_IMAGE} locally"

  AVALANCHE_PATH="${SUBNET_EVM_PATH}/../.."
  SKIP_BUILD_RACE=1 \
    DOCKER_IMAGE="${AVALANCHEGO_LOCAL_IMAGE_NAME}" \
    BUILD_MULTI_ARCH="${BUILD_MULTI_ARCH}" \
    "${AVALANCHE_PATH}"/scripts/build_image.sh
fi

GO_VERSION="$(get_go_version)"

echo "Building Docker Image: $IMAGE_NAME:$BUILD_IMAGE_ID based of AvalancheGo@$AVALANCHE_VERSION"
# Use repo root as context so Dockerfile can access graft/ directory
AVALANCHE_PATH="${SUBNET_EVM_PATH}/../.."
${DOCKER_CMD} -t "$IMAGE_NAME:$BUILD_IMAGE_ID" -t "$IMAGE_NAME:${DOCKERHUB_TAG}" \
  "$AVALANCHE_PATH" -f "$SUBNET_EVM_PATH/Dockerfile" \
  --build-arg GO_VERSION="${GO_VERSION}" \
  --build-arg AVALANCHEGO_NODE_IMAGE="$AVALANCHEGO_NODE_IMAGE" \
  --build-arg SUBNET_EVM_COMMIT="$SUBNET_EVM_COMMIT" \
  --build-arg CURRENT_BRANCH="$CURRENT_BRANCH" \
  --build-arg VM_ID="$VM_ID"

if [[ -n "${PUBLISH}" && $CURRENT_BRANCH == "master" && $ALLOW_TAG_LATEST == true ]]; then
  echo "Tagging current image as $IMAGE_NAME:latest"
  docker buildx imagetools create -t "$IMAGE_NAME:latest" "$IMAGE_NAME:$BUILD_IMAGE_ID"
fi
