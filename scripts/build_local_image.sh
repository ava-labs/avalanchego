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

avalanche_image_tag=$(docker image ls --format="{{.Tag}}" | head -n 1)

echo "Building Docker Image: $dockerhub_repo:$commit_hash"
echo "Building Docker Image: $dockerhub_repo:$avalanche_image_tag"
docker build -t "$dockerhub_repo:$commit_hash" -t "$dockerhub_repo:${avalanche_image_tag}" "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"
