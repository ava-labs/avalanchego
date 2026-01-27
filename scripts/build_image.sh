#!/usr/bin/env bash

set -euo pipefail

# Builds Docker images for avalanchego.
#
# e.g.,
# ./scripts/build_image.sh                                                              # Build local single-arch image
# ./scripts/build_image.sh --no-cache                                                   # All arguments are provided to `docker buildx build`
# SKIP_BUILD_RACE=1 ./scripts/build_image.sh                                            # Build local single-arch but skip building -r image
# DOCKER_IMAGE=myavalanchego ./scripts/build_image.sh                                   # Build local single-arch with custom image name
# PLATFORMS=linux/arm64 ./scripts/build_image.sh                                        # Build local single-arch for arm64
# PLATFORMS=linux/amd64,linux/arm64 PUSH=1 DOCKER_IMAGE=avaplatform/avalanchego ./scripts/build_image.sh  # Build and push multi-arch to docker hub
# PUSH=1 DOCKER_IMAGE=localhost:5001/avalanchego ./scripts/build_image.sh               # Build and push single-arch to private registry
# PUSH=1 DOCKER_IMAGE=localhost:5001/avalanchego FORCE_TAG_MASTER=1 ./scripts/build_image.sh  # Build and push with tag `master`

# Multi-arch builds require Docker Buildx and QEMU. buildx should be enabled by
# default in the version of docker included with Ubuntu 22.04, and qemu can be
# installed as follows:
#
#  sudo apt-get install qemu qemu-user-static
#
# After installing qemu, it will also be necessary to start a new builder that
# supports multiplatform builds and can use the host's network:
#
#  docker buildx create --use --driver-opt network=host
#
# Without `network=host`, the builder will timeout running `go mod download`.
#
# Reference: https://docs.docker.com/buildx/working-with-buildx/

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Skip building the race image
SKIP_BUILD_RACE="${SKIP_BUILD_RACE:-}"

# Force tagging as master for testing purposes
FORCE_TAG_MASTER="${FORCE_TAG_MASTER:-}"

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh
source "$AVALANCHE_PATH"/scripts/git_commit.sh
source "$AVALANCHE_PATH"/scripts/image_tag.sh
source "$AVALANCHE_PATH"/scripts/lib_build_image.sh

if [[ -z "${SKIP_BUILD_RACE}" && $image_tag == *"-r" ]]; then
  echo "Branch name must not end in '-r'"
  exit 1
fi

# The published name should be 'avaplatform/avalanchego', but to avoid unintentional
# pushes it is defaulted to 'avalanchego'.
DOCKER_IMAGE="${DOCKER_IMAGE:-avalanchego}"

# If set to non-empty, pushes the image to a registry. Otherwise, loads it locally.
# Multi-arch builds require PUSH=1 since multi-arch images cannot be loaded locally.
#
# Reference: https://docs.docker.com/build/building/multi-platform/
PUSH="${PUSH:-}"

# buildx (BuildKit) improves the speed and UI of builds over the legacy builder and
# simplifies creation of multi-arch images.
#
# Reference: https://docs.docker.com/build/buildkit/
DOCKER_CMD="docker buildx build ${*}"

# The dockerfile doesn't specify the golang version to minimize the
# changes required to bump the version. Instead, the golang version is
# provided as an argument.
GO_VERSION="$(get_go_version)"
DOCKER_CMD="${DOCKER_CMD} --build-arg GO_VERSION=${GO_VERSION}"

# Provide the git commit as a build argument to avoid requiring this
# to be discovered within the image. This enables image builds from
# git worktrees since a non-primary worktree won't have a .git
# directory to copy into the image.
DOCKER_CMD="${DOCKER_CMD} --build-arg AVALANCHEGO_COMMIT=${git_commit}"

# Configure build mode (push vs load) and platform flags
configure_docker_build_mode "${PLATFORMS:-}" "${PUSH}"
DOCKER_CMD="${DOCKER_CMD} ${DOCKER_BUILD_MODE_FLAGS} ${DOCKER_PLATFORM_FLAGS}"

echo "Building Docker Image with tags: $DOCKER_IMAGE:$commit_hash , $DOCKER_IMAGE:$image_tag"
${DOCKER_CMD} -t "$DOCKER_IMAGE:$commit_hash" -t "$DOCKER_IMAGE:$image_tag" \
              "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"

if [[ -z "${SKIP_BUILD_RACE}" ]]; then
   echo "Building Docker Image with tags (race detector): $DOCKER_IMAGE:$commit_hash-r , $DOCKER_IMAGE:$image_tag-r"
   ${DOCKER_CMD} --build-arg="RACE_FLAG=-r" -t "$DOCKER_IMAGE:$commit_hash-r" -t "$DOCKER_IMAGE:$image_tag-r" \
                 "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"
fi

# Tag latest when pushing to a registry and the tag is a release (vMAJOR.MINOR.PATCH)
if [[ "${DOCKER_IMAGE}" == *"/"* && $image_tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Tagging current avalanchego images as $DOCKER_IMAGE:latest"
  docker buildx imagetools create -t "$DOCKER_IMAGE:latest" "$DOCKER_IMAGE:$commit_hash"
fi

# Forcibly tag the image as `master` if requested. This is only intended to be used for testing.
if [[ "${DOCKER_IMAGE}" == *"/"* && -n "${FORCE_TAG_MASTER}" ]]; then
  echo "Tagging current avalanchego images as $DOCKER_IMAGE:master"
  docker buildx imagetools create -t "$DOCKER_IMAGE:master" "$DOCKER_IMAGE:$commit_hash"
fi
