#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Skip if this is not on the main public repo or
# if this is not a trusted build (Docker Credentials are not set)
if [[ $TRAVIS_REPO_SLUG != "ava-labs/avalanchego" || -z "$DOCKER_USERNAME"  ]]; then
  exit 0;
fi

# Avalanche root directory
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

current_branch=$(git rev-parse --abbrev-ref HEAD)

if [[ $current_branch == "master" ]]; then
  echo "Tagging current avalanchego image as $dockerhub_repo:latest"
  docker_image=$dockerhub_repo:latest
fi

if [[ $current_branch == "dev" ]]; then
  echo "Tagging current avalanchego image as $dockerhub_repo:dev"
  docker_image=$dockerhub_repo:dev
fi

if [[ $current_branch != "" ]]; then
  echo "Tagging current avalanchego image as $dockerhub_repo:$current_branch"
  docker_image=$dockerhub_repo:$current_branch
fi

echo "Pushing: $docker_image"

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

## pushing image with tags
docker push $docker_image
