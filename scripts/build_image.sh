#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
AOXC_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$AOXC_PATH"/scripts/constants.sh

# WARNING: this will use the most recent commit even if there are un-committed changes present
full_commit_hash="$(git --git-dir="$AOXC_PATH/.git" rev-parse HEAD)"
commit_hash="${full_commit_hash::8}"

echo "Building Docker Image with tags: $aoxc_dockerhub_repo:$commit_hash , $aoxc_dockerhub_repo:$current_branch"
docker build -t "$aoxc_dockerhub_repo:$commit_hash" \
        -t "$aoxc_dockerhub_repo:$current_branch" "$AOXC_PATH" -f "$AOXC_PATH/Dockerfile"
