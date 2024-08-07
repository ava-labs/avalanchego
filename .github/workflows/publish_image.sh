#!/usr/bin/env bash

set -euo pipefail

# If this is not a trusted build (Docker Credentials are not set)
if [[ -z "$DOCKER_USERNAME"  ]]; then
  exit 0;
fi

# Camino root directory
CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the constants
source "$CAMINO_PATH"/scripts/constants.sh

# Build current caminogo
source "$CAMINO_PATH"/scripts/build_image.sh -r

if [[ $current_branch == "master" ]]; then
  echo "Tagging current caminogo image as $camino_node_dockerhub_repo:latest"
  docker tag "$camino_node_dockerhub_repo:$current_branch" "$camino_node_dockerhub_repo:latest"
fi

echo "Pushing: $camino_node_dockerhub_repo:$current_branch"

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

## pushing image with tags
docker image push -a "$camino_node_dockerhub_repo"