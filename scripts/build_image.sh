#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/build_image.sh                                          # Build local single-arch image
# DOCKER_IMAGE=myavalanchego ./scripts/build_image.sh                # Build local single arch image with a custom image name
# DOCKER_IMAGE=avaplatform/avalanchego ./scripts/build_image.sh      # Build and push multi-arch image to docker hub
# DOCKER_IMAGE=localhost:5001/avalanchego ./scripts/build_image.sh   # Build and push multi-arch image to private registry
# DOCKER_IMAGE=localhost:5001/myavalanchego ./scripts/build_image.sh # Build and push multi-arch image to private registry with a custom image name

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
DOCKER_IMAGE=${DOCKER_IMAGE:-"avalanchego"}

DOCKER_CMD="docker buildx build"

# Multi-arch images are prompted by image names that include a slash (/). The slash
# indicates that a registry is involved, and multi-arch builds require the use of a
# registry.
#
# If an image name does not include a slash, only a single-arch image can be built.
if [[ "${DOCKER_IMAGE}" == *"/"* ]]; then
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

echo "Building Docker Image with tags: $DOCKER_IMAGE:$commit_hash , $DOCKER_IMAGE:$current_branch"
${DOCKER_CMD} -t "$DOCKER_IMAGE:$commit_hash" -t "$DOCKER_IMAGE:$current_branch" \
              "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"

echo "Building Docker Image with tags: $DOCKER_IMAGE:$commit_hash-race , $DOCKER_IMAGE:$current_branch-race"
${DOCKER_CMD} --build-arg="RACE_FLAG=-r" -t "$DOCKER_IMAGE:$commit_hash-race" -t "$DOCKER_IMAGE:$current_branch-race" \
              "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"

# Only tag the latest image for the master branch when images are pushed to a registry
if [[ "${DOCKER_IMAGE}" == *"/"* && $current_branch == "master" ]]; then
  echo "Tagging current avalanchego images as $DOCKER_IMAGE:latest"
  docker buildx imagetools create -t "$DOCKER_IMAGE:latest" "$DOCKER_IMAGE:$commit_hash"
fi
