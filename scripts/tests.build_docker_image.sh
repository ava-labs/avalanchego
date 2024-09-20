#!/usr/bin/env bash

set -euo pipefail

# Sanity check the image build by attempting to build and run the image without error.

# Directory above this script
SUBNET_EVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

# Load the versions
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Use the default node image
AVALANCHEGO_NODE_IMAGE="${AVALANCHEGO_IMAGE_NAME}:${AVALANCHE_VERSION}"

# Build the avalanchego image if it cannot be pulled. This will usually be due to
# AVALANCHE_VERSION being not yet merged since the image is published post-merge.
if ! docker pull "${AVALANCHEGO_NODE_IMAGE}"; then
  # Use a image name without a repository (i.e. without 'avaplatform/' prefix ) to build a
  # local image that will not be pushed.
  export AVALANCHEGO_IMAGE_NAME="avalanchego"
  echo "Building ${AVALANCHEGO_IMAGE_NAME}:${AVALANCHE_VERSION} locally"

  source "${SUBNET_EVM_PATH}"/scripts/lib_avalanchego_clone.sh
  clone_avalanchego "${AVALANCHE_VERSION}"
  SKIP_BUILD_RACE=1 DOCKER_IMAGE="${AVALANCHEGO_IMAGE_NAME}" "${AVALANCHEGO_CLONE_PATH}"/scripts/build_image.sh
fi

# Build a local image
bash -x "${SUBNET_EVM_PATH}"/scripts/build_docker_image.sh

# Check that the image can be run and contains the plugin
echo "Checking version of the plugin provided by the image"
docker run -t --rm "${DOCKERHUB_REPO}:${DOCKERHUB_TAG}" /avalanchego/build/plugins/"${DEFAULT_VM_ID}" --version
echo "" # --version output doesn't include a newline
echo "Successfully checked image build"
