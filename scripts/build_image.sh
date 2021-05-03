#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanche root directory
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# WARNING: this will use the most recent commit even if there are un-committed changes present
FULL_CORETH_COMMIT="$(git --git-dir="$AVALANCHE_PATH/.git" rev-parse HEAD)"
# Use an abbreviated version of the full commit to tag the image.
CORETH_COMMIT="${FULL_CORETH_COMMIT::8}"

echo "Building Docker Image: $dockerhub_repo:$avalanche_version-$CORETH_COMMIT"
docker build -t "$dockerhub_repo:$avalanche_version-$CORETH_COMMIT" "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile" \
  --build-arg AVALANCHE_VERSION="$avalanche_version" \
  --build-arg CORETH_COMMIT="$FULL_CORETH_COMMIT" \
  --build-arg CURRENT_BRANCH="$current_branch"
