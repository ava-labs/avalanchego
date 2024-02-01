#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
CAMINO_NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$CAMINO_NODE_PATH"/scripts/constants.sh

echo "Building Docker Image with tag $camino_node_dockerhub_repo:$current_branch"
docker build -t "$camino_node_dockerhub_repo:$current_branch" "$CAMINO_NODE_PATH" -f "$CAMINO_NODE_PATH/Dockerfile"
