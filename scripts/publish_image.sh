#!/usr/bin/env bash

set -euo pipefail

if [[ -n "${DOCKER_USERNAME:-}"  ]]; then
  # If this is a trusted build that is expected to push images to dockerhub
  echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
fi

# Avalanche root directory
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# Build current avalanchego
# source "$AVALANCHE_PATH"/scripts/build_image.sh

if [[ $current_branch == "master" ]]; then
  echo "Tagging current avalanchego images as $avalanchego_dockerhub_repo:latest-<arch>"
  for arch in "${TARGET_ARCHITECTURES[@]}"; do
    docker tag "$avalanchego_dockerhub_repo:$COMMIT_HASH-$arch" "$avalanchego_dockerhub_repo:latest-$arch"
  done
fi

## pushing image with tags
docker image push -a "$avalanchego_dockerhub_repo"

create_multi_arch_image() {
  local tag=$1

  local repo="$avalanchego_dockerhub_repo"

  echo "Creating multi-arch Docker Image $repo:$tag"

  local images=()
  for arch in "${TARGET_ARCHITECTURES[@]}"; do
    images+=("$repo:$tag-$arch")
  done

  # shellcheck disable=SC2068
  docker manifest create "$repo:$tag" ${images[@]}

  # Annotate the manifest with the OS and Architecture for each image
  for image in "${images[@]}"; do
    local arch
    arch=$(echo "$image" | rev | cut -d- -f1 | rev)
    docker manifest annotate "$repo:$tag" "$image" --os linux --arch "$arch"
  done

  docker manifest push "$repo:$tag"
}

# Create multi-arch images
create_multi_arch_image "$COMMIT_HASH"
create_multi_arch_image "$current_branch-race"
if [[ $current_branch == "master" ]]; then
  create_multi_arch_image latest
fi
create_multi_arch_image "$COMMIT_HASH-race"
create_multi_arch_image "$current_branch-race"
