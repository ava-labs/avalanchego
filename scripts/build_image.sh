#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Build AvalancheGo image using local verion of coreth and the AvalancheGo version set here.
SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"
AVALANCHE_VERSION="v1.3.2"
CORETH_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
DOCKERHUB_REPO="avaplatform/avalanchego"

# WARNING: this will use the most recent commit even if there are un-committed changes present
FULL_CORETH_COMMIT="$(git --git-dir="$CORETH_PATH/.git" rev-parse HEAD)"
# Use an abbreviated version of the full commit to tag the image.
CORETH_COMMIT="${FULL_CORETH_COMMIT::8}"

echo "Building Docker Image: $DOCKERHUB_REPO:$AVALANCHE_VERSION-$CORETH_COMMIT"
docker build -t "$DOCKERHUB_REPO:$AVALANCHE_VERSION-$CORETH_COMMIT" "$CORETH_PATH" -f "$CORETH_PATH/Dockerfile" --build-arg AVALANCHE_VERSION="$AVALANCHE_VERSION" --build-arg CORETH_COMMIT="$FULL_CORETH_COMMIT"
