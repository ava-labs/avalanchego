#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
CAMINOGO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$CAMINOGO_PATH"/scripts/constants.sh

echo "Building Docker Image with tag $camino_node_dockerhub_repo:$current_branch"
docker build -t "$camino_node_dockerhub_repo:$current_branch" "$CAMINOGO_PATH" -f "$CAMINOGO_PATH/Dockerfile"