#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/build_image.sh                                                            # Build local single-arch image
# ./scripts/build_image.sh --no-cache                                                 # All arguments are provided to `docker buildx build`
# SKIP_BUILD_RACE=1 ./scripts/build_image.sh                                          # Build local single-arch image but skip building -r image
# DOCKER_IMAGE=myavalanchego ./scripts/build_image.sh                                 # Build local single arch image with a custom image name
# DOCKER_IMAGE=avaplatform/avalanchego ./scripts/build_image.sh                       # Build and push multi-arch image to docker hub
# DOCKER_IMAGE=localhost:5001/avalanchego ./scripts/build_image.sh                    # Build and push multi-arch image to private registry
# DOCKER_IMAGE=localhost:5001/avalanchego FORCE_TAG_LATEST=1 ./scripts/build_image.sh # Build and push image to private registry with tag `latest`

# Multi-arch builds require Docker Buildx and QEMU. buildx should be enabled by
# default in the verson of docker included with Ubuntu 22.04, and qemu can be
# installed as follows:
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

# Skip building the race image
SKIP_BUILD_RACE="${SKIP_BUILD_RACE:-}"

# Force tagging as latest even if not the master branch
FORCE_TAG_LATEST="${FORCE_TAG_LATEST:-}"

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

if [[ -z "${SKIP_BUILD_RACE}" && $image_tag == *"-r" ]]; then
  echo "Branch name must not end in '-r'"
  exit 1
fi

# The published name should be 'avaplatform/avalanchego', but to avoid unintentional
# pushes it is defaulted to 'avalanchego' (without a repo or registry name) which can
# only be used to create local images.
DOCKER_IMAGE="${DOCKER_IMAGE:-avalanchego}"

# If set to non-empty, prompts the building of a multi-arch image when the image
# name indicates use of a registry.
#
# A registry is required to build a multi-arch image since a multi-arch image is
# not really an image at all. A multi-arch image (also called a manifest) is
# basically a list of arch-specific images available from the same registry that
# hosts the manifest. Manifests are not supported for local images.
#
# Reference: https://docs.docker.com/build/building/multi-platform/
BUILD_MULTI_ARCH="${BUILD_MULTI_ARCH:-}"

# buildx (BuildKit) improves the speed and UI of builds over the legacy builder and
# simplifies creation of multi-arch images.
#
# Reference: https://docs.docker.com/build/buildkit/
DOCKER_CMD="docker buildx build ${*}"

# The dockerfile doesn't specify the golang version to minimize the
# changes required to bump the version. Instead, the golang version is
# provided as an argument.
GO_VERSION="$(go list -m -f '{{.GoVersion}}')"
DOCKER_CMD="${DOCKER_CMD} --build-arg GO_VERSION=${GO_VERSION}"

if [[ "${DOCKER_IMAGE}" == *"/"* ]]; then
  # Default to pushing when the image name includes a slash which indicates the
  # use of a registry e.g.
  #
  #  - dockerhub: [repo]/[image name]:[tag]
  #  - private registry: [private registry hostname]/[image name]:[tag]
  DOCKER_CMD="${DOCKER_CMD} --push"

  # Build a multi-arch image if requested
  if [[ -n "${BUILD_MULTI_ARCH}" ]]; then
    DOCKER_CMD="${DOCKER_CMD} --platform=${PLATFORMS:-linux/amd64,linux/arm64}"
  fi

  # A populated DOCKER_USERNAME env var triggers login
  if [[ -n "${DOCKER_USERNAME:-}" ]]; then
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
  fi
else
  # Build a single-arch image since the image name does not include a slash which
  # indicates that a registry is not available.
  #
  # Building a single-arch image with buildx and having the resulting image show up
  # in the local store of docker images (ala 'docker build') requires explicitly
  # loading it from the buildx store with '--load'.
  DOCKER_CMD="${DOCKER_CMD} --load"
fi

echo "Building Docker Image with tags: $DOCKER_IMAGE:$commit_hash , $DOCKER_IMAGE:$image_tag"
${DOCKER_CMD} -t "$DOCKER_IMAGE:$commit_hash" -t "$DOCKER_IMAGE:$image_tag" \
              "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"

if [[ -z "${SKIP_BUILD_RACE}" ]]; then
   echo "Building Docker Image with tags (race detector): $DOCKER_IMAGE:$commit_hash-r , $DOCKER_IMAGE:$image_tag-r"
   ${DOCKER_CMD} --build-arg="RACE_FLAG=-r" -t "$DOCKER_IMAGE:$commit_hash-r" -t "$DOCKER_IMAGE:$image_tag-r" \
                 "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"
fi

# Only tag the latest image for the master branch when images are pushed to a registry
if [[ "${DOCKER_IMAGE}" == *"/"* && ($image_tag == "master" || -n "${FORCE_TAG_LATEST}") ]]; then
  echo "Tagging current avalanchego images as $DOCKER_IMAGE:latest"
  docker buildx imagetools create -t "$DOCKER_IMAGE:latest" "$DOCKER_IMAGE:$commit_hash"
fi
