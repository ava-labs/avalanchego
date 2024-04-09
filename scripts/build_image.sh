#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/build_image.sh                                              # Build local single-arch image
# DOCKER_REPO=avaplatform/avalanchego ./scripts/build_image.sh          # Build and push multi-arch image to docker hub
# DOCKER_REPO=localhost:5001/avalanchego ./scripts/build_image.sh       # Build and push multi-arch image to private registry

# Multi-arch builds require Docker Buildx and QEMU. buildx should be
# enabled by default as of this writing, and qemu can be installed on
# ubuntu as follows:
#
#  sudo apt-get install qemu qemu-user-static
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

DOCKER_CMD="docker build" # Default to a local single-arch image
if [[ "${avalanchego_dockerhub_repo}" == *"/"* ]]; then
  # Assume that the image name containing a '/' (indicating use of a registry) requires pushing a multi-arch image to a registry
  PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
  DOCKER_CMD="docker buildx build --push --platform=${PLATFORMS}"

  if [[ -n "${DOCKER_USERNAME:-}" ]]; then
    # Assume the registry requires authentication
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
  fi
fi

echo "Building Docker Image with tags: $avalanchego_dockerhub_repo:$commit_hash, $avalanchego_dockerhub_repo:$current_branch"
${DOCKER_CMD} -t "$avalanchego_dockerhub_repo:$commit_hash" \
        -t "$avalanchego_dockerhub_repo:$current_branch" "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"

echo "Building Docker Image with tags: $avalanchego_dockerhub_repo:$commit_hash-race , $avalanchego_dockerhub_repo:$current_branch-race"
${DOCKER_CMD} --build-arg="RACE_FLAG=-r" -t "$avalanchego_dockerhub_repo:$commit_hash-race" \
        -t "$avalanchego_dockerhub_repo:$current_branch-race" "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"

# Only tag the latest image for the master branch when images are pushed to a registry
if [[ "${avalanchego_dockerhub_repo}" == *"/"* && $current_branch == "master" ]]; then
  echo "Tagging current avalanchego images as $avalanchego_dockerhub_repo:latest"
  docker buildx imagetools create -t "$avalanchego_dockerhub_repo:latest" "$avalanchego_dockerhub_repo:$commit_hash"
fi
