#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# If this is not a trusted build (Docker Credentials are not set)
if [[ -z "$DOCKER_USERNAME"  ]]; then
  exit 0;
fi

# Avalanche root directory
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

if [[ $current_branch == "master" ]]; then
  echo "Tagging current avalanchego image as $avalanchego_dockerhub_repo:latest"
  docker tag $avalanchego_dockerhub_repo:$current_branch $avalanchego_dockerhub_repo:latest
fi

echo "Pushing: $avalanchego_dockerhub_repo:$current_branch"

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

## pushing image with tags
docker image push -a $avalanchego_dockerhub_repo
