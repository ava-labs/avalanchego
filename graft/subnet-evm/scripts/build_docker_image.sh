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

# buildx (BuildKit) improves the speed and UI of builds over the legacy builder and
# simplifies creation of multi-arch images.
#
# Reference: https://docs.docker.com/build/buildkit/
DOCKER_CMD="docker buildx build"
ispush=0
if [[ -n "${PUBLISH}" ]]; then
  echo "Pushing $IMAGE_NAME:$BUILD_IMAGE_ID"
  ispush=1
  # A populated DOCKER_USERNAME env var triggers login
  if [[ -n "${DOCKER_USERNAME:-}" ]]; then
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
  fi
fi

# Build a specified platform image if requested
if [[ -n "${PLATFORMS}" ]]; then
  DOCKER_CMD="${DOCKER_CMD} --platform=${PLATFORMS}"
  if [[ "$PLATFORMS" == *,* ]]; then ## Multi-arch
    if [[ "${IMAGE_NAME}" != *"/"* ]]; then
      echo "ERROR: Multi-arch images (multi-platform) must be pushed to a registry."
      exit 1
    fi
    ispush=1
  fi
fi

if [[ $ispush -eq 1 ]]; then
  DOCKER_CMD="${DOCKER_CMD} --push"
else
  ## Single arch
  #
  # Building a single-arch image with buildx and having the resulting image show up
  # in the local store of docker images (ala 'docker build') requires explicitly
  # loading it from the buildx store with '--load'.
  DOCKER_CMD="${DOCKER_CMD} --load"
fi

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

  if [[ -n "${BUILD_MULTI_ARCH}" && "${AVALANCHEGO_LOCAL_IMAGE_NAME}" != *"/"* ]]; then
    echo "ERROR: Multi-arch images must be pushed to a registry."
    exit 1
  fi

  AVALANCHEGO_NODE_IMAGE="${AVALANCHEGO_LOCAL_IMAGE_NAME}:${AVALANCHE_VERSION}"
  echo "Building ${AVALANCHEGO_NODE_IMAGE} locally"

  AVALANCHE_PATH="${SUBNET_EVM_PATH}/../.."
  SKIP_BUILD_RACE=1 \
    DOCKER_IMAGE="${AVALANCHEGO_LOCAL_IMAGE_NAME}" \
    BUILD_MULTI_ARCH="${BUILD_MULTI_ARCH}" \
    "${AVALANCHE_PATH}"/scripts/build_image.sh
fi

# Use head -1 because go workspaces list multiple modules; CI validates
# all modules use the same Go version.
GO_VERSION="$(go list -m -f '{{.GoVersion}}' | head -1)"

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

# Tag latest when pushing to a registry and the tag is a stable release (vMAJOR.MINOR.PATCH)
if [[ "${IMAGE_NAME}" == *"/"* && $CURRENT_BRANCH =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Tagging current image as $IMAGE_NAME:latest"
  docker buildx imagetools create -t "$IMAGE_NAME:latest" "$IMAGE_NAME:$BUILD_IMAGE_ID"
fi
