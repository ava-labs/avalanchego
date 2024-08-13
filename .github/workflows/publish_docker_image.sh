#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# If this is not a trusted build (Docker Credentials are not set)
if [[ -z "$DOCKER_USERNAME"  ]]; then
  exit 0;
fi

# Avalanche root directory
SUBNET_EVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the versions
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Set the vm id if provided
if [[ $# -eq 1 ]]; then
    VM_ID=$1
fi

# Buld the docker image
source "$SUBNET_EVM_PATH"/scripts/build_docker_image.sh

if [[ $CURRENT_BRANCH == "master" ]]; then
  echo "Tagging current image as $DOCKERHUB_REPO:latest"
  docker tag "$DOCKERHUB_REPO:$BUILD_IMAGE_ID" "$DOCKERHUB_REPO:latest"
fi

echo "Pushing $DOCKERHUB_REPO:$BUILD_IMAGE_ID"

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

docker push -a "$DOCKERHUB_REPO"
