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

# WARNING: this will use the most recent commit even if there are un-committed changes present
full_commit_hash="$(git --git-dir="$CAMINO_PATH/.git" rev-parse HEAD)"
commit_hash="${full_commit_hash::8}"

echo "Building Docker Image with tags: $caminogo_dockerhub_repo:$commit_hash , $caminogo_dockerhub_repo:$current_branch"
docker build -t "$caminogo_dockerhub_repo:$commit_hash" \
        -t "$caminogo_dockerhub_repo:$current_branch" "$CAMINO_PATH" -f "$CAMINO_PATH/Dockerfile"
