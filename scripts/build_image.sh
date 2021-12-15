#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanche root directory
SUBNET_EVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

echo "Building Docker Image: $dockerhub_repo:$build_image_id based of $avalanche_version"
docker build -t "$dockerhub_repo:$build_image_id" "$SUBNET_EVM_PATH" -f "$SUBNET_EVM_PATH/Dockerfile" \
  --build-arg AVALANCHE_VERSION="$avalanche_version" \
  --build-arg SUBNET_EVM_COMMIT="$subnet_evm_commit" \
  --build-arg CURRENT_BRANCH="$current_branch"
