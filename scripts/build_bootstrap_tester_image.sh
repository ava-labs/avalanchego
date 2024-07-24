#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

source ./scripts/constants.sh

IMAGE_NAME="bootstrap-tester"

IMAGE_TAG="${IMAGE_TAG:-}"
if [[ -z "${IMAGE_TAG}" ]]; then
  # Default to tagging with the commit hash
  IMAGE_TAG="${commit_hash}"
fi

# Build the avalanchego image
DOCKER_CMD="docker buildx build"

# Specifying an image prefix will ensure the image is pushed after build
IMAGE_PREFIX="${IMAGE_PREFIX:-}"
if [[ -n "${IMAGE_PREFIX}" ]]; then
  IMAGE_NAME="${IMAGE_PREFIX}/${IMAGE_NAME}"
  DOCKER_CMD="${DOCKER_CMD} --push"

  # Tag the image as latest for the master branch
  if [[ "${image_tag}" == "master" ]]; then
    DOCKER_CMD="${DOCKER_CMD} -t ${IMAGE_NAME}:latest"
  fi

  # A populated DOCKER_USERNAME env var triggers login
  if [[ -n "${DOCKER_USERNAME:-}" ]]; then
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
  fi

  # The avalanchego image will have already have been built
  AVALANCHEGO_NODE_IMAGE="${IMAGE_PREFIX}/avalanchego:${IMAGE_TAG}"
else
  # Build the avalanchego image locally
  ./scripts/build_image.sh
  AVALANCHEGO_NODE_IMAGE="avalanchego:${IMAGE_TAG}"
fi

# The dockerfiles don't specify the golang version to minimize the changes required to bump
# the version. Instead, the golang version is provided as an argument.
GO_VERSION="$(go list -m -f '{{.GoVersion}}')"

# Build the image for the bootstrap tester
${DOCKER_CMD} -t "${IMAGE_NAME}:${IMAGE_TAG}" \
              --build-arg GO_VERSION="${GO_VERSION}" --build-arg AVALANCHEGO_NODE_IMAGE="${AVALANCHEGO_NODE_IMAGE}" \
              -f "${AVALANCHE_PATH}/tests/bootstrap/Dockerfile" "${AVALANCHE_PATH}"
