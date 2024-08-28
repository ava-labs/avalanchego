#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
SUBNET_EVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

# Load the versions
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# WARNING: this will use the most recent commit even if there are un-committed changes present
BUILD_IMAGE_ID=${BUILD_IMAGE_ID:-"${CURRENT_BRANCH}"}

VM_ID=${VM_ID:-"${DEFAULT_VM_ID}"}
if [[ "${VM_ID}" != "${DEFAULT_VM_ID}"  ]]; then
  DOCKERHUB_TAG="${VM_ID}-${DOCKERHUB_TAG}"
fi

# Default to the release image. Will need to be overridden when testing against unreleased versions.
AVALANCHEGO_NODE_IMAGE=${AVALANCHEGO_NODE_IMAGE:-"avaplatform/avalanchego:${AVALANCHE_VERSION}"}

echo "Building Docker Image: $DOCKERHUB_REPO:$BUILD_IMAGE_ID based of AvalancheGo@$AVALANCHE_VERSION"
docker build -t "$DOCKERHUB_REPO:$BUILD_IMAGE_ID" -t "$DOCKERHUB_REPO:${DOCKERHUB_TAG}" \
 "$SUBNET_EVM_PATH" -f "$SUBNET_EVM_PATH/Dockerfile" \
  --build-arg AVALANCHEGO_NODE_IMAGE="$AVALANCHEGO_NODE_IMAGE" \
  --build-arg SUBNET_EVM_COMMIT="$SUBNET_EVM_COMMIT" \
  --build-arg CURRENT_BRANCH="$CURRENT_BRANCH" \
  --build-arg VM_ID="$VM_ID"