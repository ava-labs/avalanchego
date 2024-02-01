#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Camino root directory
CAMINO_NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Build the container
"$CAMINO_NODE_PATH"/scripts/build_local_image.sh

# If this is not a trusted build (Docker Credentials are not set)
if [[ -z "$DOCKER_USERNAME"  ]]; then
  exit 0;
fi

# Load the constants
source "$CAMINO_NODE_PATH"/scripts/constants.sh

echo "Pushing: $camino_node_dockerhub_repo:$current_branch"

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

## pushing image with tags
docker image push -a $camino_node_dockerhub_repo
