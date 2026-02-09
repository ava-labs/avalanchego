#!/usr/bin/env bash

set -euo pipefail

# Builds Docker images for avalanchego or subnet-evm using a single multi-stage Dockerfile.
#
# TARGET controls which image to build:
#   TARGET=avalanchego        (default) - builds the avalanchego node image
#   TARGET=subnet-evm                   - builds the subnet-evm plugin image
#
# e.g.,
# ./scripts/build_image.sh                                                              # Build local avalanchego image
# ./scripts/build_image.sh --no-cache                                                   # Extra arguments are passed to `docker buildx build`
# SKIP_BUILD_RACE=1 ./scripts/build_image.sh                                            # Skip building -r (race detector) image
# DOCKER_IMAGE=myavalanchego ./scripts/build_image.sh                                   # Build with a custom image name
# DOCKER_IMAGE=avaplatform/avalanchego ./scripts/build_image.sh                         # Build and push multi-arch to docker hub
# DOCKER_IMAGE=localhost:5001/avalanchego ./scripts/build_image.sh                      # Build and push to private registry
# DOCKER_IMAGE=localhost:5001/avalanchego FORCE_TAG_MASTER=1 ./scripts/build_image.sh   # Push with tag `master`
# TARGET=subnet-evm ./scripts/build_image.sh                                            # Build local subnet-evm image
# TARGET=subnet-evm DOCKER_IMAGE=avaplatform/subnet-evm_avalanchego ./scripts/build_image.sh # Build and push subnet-evm

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

TARGET="${TARGET:-avalanchego}"
SKIP_BUILD_RACE="${SKIP_BUILD_RACE:-}"
FORCE_TAG_MASTER="${FORCE_TAG_MASTER:-}"
ALLOW_TAG_LATEST="${ALLOW_TAG_LATEST:-}"

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

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh
source "$AVALANCHE_PATH"/scripts/git_commit.sh
source "$AVALANCHE_PATH"/scripts/image_tag.sh

# All target-specific variables are set in this single block for readability.
# Names without a slash create local-only images to avoid unintentional pushes.
# Names with a slash trigger a push to the registry.
target_build_args=()
case "${TARGET}" in
  avalanchego)
    DOCKER_IMAGE="${DOCKER_IMAGE:-avalanchego}"
    # Provide the git commit as a build argument to avoid requiring this to be discovered
    # within the image. This enables image builds from git worktrees since a non-primary
    # worktree won't have a .git directory to copy into the image.
    target_build_args=(--build-arg "AVALANCHEGO_COMMIT=${git_commit}")
    ;;
  subnet-evm)
    DOCKER_IMAGE="${DOCKER_IMAGE:-subnet-evm_avalanchego}"
    SKIP_BUILD_RACE=1

    # Determine the base avalanchego node image. Priority:
    #   1. AVALANCHEGO_NODE_IMAGE (explicit full image reference)
    #   2. AVALANCHEGO_NODE_IMAGE_REPO:image_tag (repo prefix, e.g. from CI)
    #   3. avalanchego:image_tag (local image, auto-built if missing)
    if [[ -z "${AVALANCHEGO_NODE_IMAGE:-}" && -n "${AVALANCHEGO_NODE_IMAGE_REPO:-}" ]]; then
      AVALANCHEGO_NODE_IMAGE="${AVALANCHEGO_NODE_IMAGE_REPO}:${image_tag}"
    elif [[ -z "${AVALANCHEGO_NODE_IMAGE:-}" ]]; then
      AVALANCHEGO_NODE_IMAGE="avalanchego:${image_tag}"
      # Build the local avalanchego image if it doesn't exist yet
      if ! docker image inspect "${AVALANCHEGO_NODE_IMAGE}" > /dev/null 2>&1; then
        echo "Base image ${AVALANCHEGO_NODE_IMAGE} not found locally, building it..."
        TARGET=avalanchego SKIP_BUILD_RACE=1 DOCKER_IMAGE=avalanchego "$AVALANCHE_PATH/scripts/build_image.sh"
      fi
    fi

    VM_ID="${VM_ID:-srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy}"
    target_build_args=(
      --build-arg "AVALANCHEGO_NODE_IMAGE=${AVALANCHEGO_NODE_IMAGE}"
      --build-arg "VM_ID=${VM_ID}"
    )

    ;;
  *)
    echo "Unknown TARGET: ${TARGET}. Valid values: avalanchego, subnet-evm"
    exit 1
    ;;
esac

if [[ -z "${SKIP_BUILD_RACE}" && $image_tag == *"-r" ]]; then
  echo "Branch name must not end in '-r'"
  exit 1
fi

# Build the docker command as an array for safe expansion.
#
# buildx (BuildKit) improves the speed and UI of builds over the legacy builder and
# simplifies creation of multi-arch images.
#
# Reference: https://docs.docker.com/build/buildkit/
docker_cmd=(docker buildx build "$@")
docker_cmd+=(--build-arg "GO_VERSION=${GO_VERSION}")
docker_cmd+=("${target_build_args[@]}")

if [[ "${DOCKER_IMAGE}" == *"/"* ]]; then
  # Default to pushing when the image name includes a slash which indicates the
  # use of a registry e.g.
  #
  #  - dockerhub: [repo]/[image name]:[tag]
  #  - private registry: [private registry hostname]/[image name]:[tag]
  docker_cmd+=(--push)

  # Build a multi-arch image if requested
  if [[ -n "${BUILD_MULTI_ARCH}" ]]; then
    docker_cmd+=(--platform="${PLATFORMS:-linux/amd64,linux/arm64}")
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
  docker_cmd+=(--load)
fi

# Build the image
echo "Building Docker Image (${TARGET}): $DOCKER_IMAGE:$commit_hash, $DOCKER_IMAGE:$image_tag"
"${docker_cmd[@]}" --target="${TARGET}" -t "$DOCKER_IMAGE:$commit_hash" -t "$DOCKER_IMAGE:$image_tag" \
              "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"

# Build race detector variant (avalanchego only)
if [[ -z "${SKIP_BUILD_RACE}" ]]; then
   echo "Building Docker Image (${TARGET}, race): $DOCKER_IMAGE:$commit_hash-r, $DOCKER_IMAGE:$image_tag-r"
   "${docker_cmd[@]}" --target="${TARGET}" --build-arg="RACE_FLAG=-r" -t "$DOCKER_IMAGE:$commit_hash-r" -t "$DOCKER_IMAGE:$image_tag-r" \
                 "$AVALANCHE_PATH" -f "$AVALANCHE_PATH/Dockerfile"
fi

# Tag latest when pushing to a registry
if [[ "${DOCKER_IMAGE}" == *"/"* ]]; then
  case "${TARGET}" in
    avalanchego)
      # Tag latest for release versions (vMAJOR.MINOR.PATCH)
      if [[ $image_tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "Tagging $DOCKER_IMAGE:latest"
        docker buildx imagetools create -t "$DOCKER_IMAGE:latest" "$DOCKER_IMAGE:$commit_hash"
      fi
      # Forcibly tag as master if requested (testing only)
      if [[ -n "${FORCE_TAG_MASTER}" ]]; then
        echo "Tagging $DOCKER_IMAGE:master"
        docker buildx imagetools create -t "$DOCKER_IMAGE:master" "$DOCKER_IMAGE:$commit_hash"
      fi
      ;;
    subnet-evm)
      if [[ $image_tag == "master" && "${ALLOW_TAG_LATEST}" == true ]]; then
        echo "Tagging $DOCKER_IMAGE:latest"
        docker buildx imagetools create -t "$DOCKER_IMAGE:latest" "$DOCKER_IMAGE:$commit_hash"
      fi
      ;;
  esac
fi
