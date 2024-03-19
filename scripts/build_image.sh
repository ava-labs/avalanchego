#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanche root directory
SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Load the versions
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

BUILD_IMAGE_ID=${BUILD_IMAGE_ID:-"avalanchego-${AVALANCHE_VERSION}-subnet-evm-${CURRENT_BRANCH}"}

echo "Building Docker Image: $DOCKERHUB_REPO:$BUILD_IMAGE_ID based of $AVALANCHE_VERSION"
docker build -t "$DOCKERHUB_REPO:$BUILD_IMAGE_ID" "$SUBNET_EVM_PATH" -f "$SUBNET_EVM_PATH/Dockerfile" \
  --build-arg AVALANCHE_VERSION="$AVALANCHE_VERSION" \
  --build-arg SUBNET_EVM_COMMIT="$SUBNET_EVM_COMMIT" \
  --build-arg CURRENT_BRANCH="$CURRENT_BRANCH"

if [[ ${PUSH_DOCKER_IMAGE:-""} == "true" ]]; then
  if [[ $CURRENT_BRANCH == "master" ]]; then
    echo "Tagging current image as $DOCKERHUB_REPO:latest"
    docker tag "$DOCKERHUB_REPO:$BUILD_IMAGE_ID" "$DOCKERHUB_REPO:latest"
  fi
  docker push "$DOCKERHUB_REPO:$BUILD_IMAGE_ID"
fi
