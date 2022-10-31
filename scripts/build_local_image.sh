#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$CAMINO_PATH"/scripts/versions.sh

# Load the constants
source "$CAMINO_PATH"/scripts/constants.sh

echo "Building Docker Image with tag $caminogo_dockerhub_repo:$current_branch"
docker build -t "$caminogo_dockerhub_repo:$current_branch" "$CAMINO_PATH" -f "$CAMINO_PATH/Dockerfile"
