#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# WARNING: this will use the most recent commit even if there are un-committed changes present
full_commit_hash="$(git --git-dir="$AVALANCHE_PATH/.git" rev-parse HEAD)"
commit_hash="${full_commit_hash::8}"

echo "Building Docker Image with tags: $avalanchego_dockerhub_repo:$commit_hash , $avalanchego_dockerhub_repo:$current_branch"
docker build -t "$avalanchego_dockerhub_repo:$commit_hash" \
        -t "$avalanchego_dockerhub_repo:$current_branch" "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"
