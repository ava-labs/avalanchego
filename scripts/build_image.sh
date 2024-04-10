#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/build_image.sh                                          # Build local single-arch image
# DOCKER_REPO=myavalanchego ./scripts/build_image.sh                # Build local single arch image with a custom image name
# DOCKER_REPO=avaplatform/avalanchego ./scripts/build_image.sh      # Build and push multi-arch image to docker hub
# DOCKER_REPO=localhost:5001/avalanchego ./scripts/build_image.sh   # Build and push multi-arch image to private registry
# DOCKER_REPO=localhost:5001/myavalanchego ./scripts/build_image.sh # Build and push multi-arch image to private registry with a custom image name

# Multi-arch builds require Docker Buildx and QEMU. buildx should be enabled by
# default as of this writing, and qemu can be installed on ubuntu as follows:
#
#  sudo apt-get install qemu qemu-user-static
#
# After installing qemu, it will also be necessary to start a new builder that can
# support multiplatform builds:
#
#  docker buildx create --use
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

# The published name should be 'avaplatform/avalanchego', but to avoid unintentional
# pushes it is defaulted to 'avalanchego' (without the registry name) which is a
# local-only name.
#
# TODO(marun) Rename both avalanchego_dockerhub_repo and DOCKER_REPO to IMAGE_NAME for clarity
avalanchego_dockerhub_repo=${DOCKER_REPO:-"avalanchego"}

DOCKER_CMD="docker buildx build"

# Multi-arch images are prompted by image names that include a slash (/). The slash
# indicates that a registry is involved, and multi-arch builds require the use of a
# registry.
#
# If an image name does not include a slash, only a single-arch image can be built.
if [[ "${avalanchego_dockerhub_repo}" == *"/"* ]]; then
  PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
  DOCKER_CMD="${DOCKER_CMD} --push --platform=${PLATFORMS}"

  # A populated DOCKER_USERNAME env var triggers login
  if [[ -n "${DOCKER_USERNAME:-}" ]]; then
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
  fi
else
  # Building a single-arch image with buildx and having the resulting image show up
  # in the local store of docker images (ala 'docker build') requires explicitly
  # loading it from the buildx store with '--load'.
  DOCKER_CMD="${DOCKER_CMD} --load"
fi

echo "Building Docker Image with tags: $avalanchego_dockerhub_repo:$commit_hash , $avalanchego_dockerhub_repo:$current_branch"
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
