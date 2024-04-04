#!/usr/bin/env bash

set -euo pipefail

# Cross-platform builds require Docker Buildx and QEMU. buildx should
# be enabled by default as of this writing, and qemu can be installed
# on ubuntu as follows:
#
#  sudo apt-get install qemu qemu-user-static
#
#
# Reference: https://docs.docker.com/buildx/working-with-buildx/

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

if [[ $current_branch == *"-race" ]]; then
  echo "Branch name must not end in '-race'"
  exit 1
fi

# WARNING: this will use the most recent commit even if there are un-committed changes present
full_commit_hash="$(git --git-dir="$AVALANCHE_PATH/.git" rev-parse HEAD)"
# Export to ensure the variable is available to the caller
export COMMIT_HASH="${full_commit_hash::8}"

# Build images for supported architectures
TARGET_ARCHITECTURES=("amd64" "arm64")
for TARGET_ARCH in "${TARGET_ARCHITECTURES[@]}"; do
  # Only linux is supported
  PLATFORM="linux/$TARGET_ARCH"

  echo "Building Docker Image with tags: $avalanchego_dockerhub_repo:$COMMIT_HASH-$TARGET_ARCH , $avalanchego_dockerhub_repo:$current_branch-$TARGET_ARCH"
  docker buildx build --platform="$PLATFORM" \
         -t "$avalanchego_dockerhub_repo:$COMMIT_HASH-$TARGET_ARCH" -t "$avalanchego_dockerhub_repo:$current_branch-$TARGET_ARCH" \
         "$AVALANCHE_PATH" --load -f "$AVALANCHE_PATH/Dockerfile"

  echo "Building Docker Image with tags: $avalanchego_dockerhub_repo:$COMMIT_HASH-race-$TARGET_ARCH , $avalanchego_dockerhub_repo:$current_branch-race-$TARGET_ARCH"
  docker buildx build --platform="$PLATFORM" --build-arg="RACE_FLAG=-r" \
         -t "$avalanchego_dockerhub_repo:$COMMIT_HASH-race-$TARGET_ARCH" -t "$avalanchego_dockerhub_repo:$current_branch-race-$TARGET_ARCH" \
         "$AVALANCHE_PATH" --load -f "$AVALANCHE_PATH/Dockerfile"
done
