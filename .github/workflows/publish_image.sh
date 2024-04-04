#!/usr/bin/env bash

set -euo pipefail

# If this is not a trusted build (Docker Credentials are not set)
if [[ -z "$DOCKER_USERNAME"  ]]; then
  exit 0;
fi

# Avalanche root directory
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# Build current avalanchego
source "$AVALANCHE_PATH"/scripts/build_image.sh

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

if [[ $current_branch == "master" ]]; then
  echo "Tagging current avalanchego images as $avalanchego_dockerhub_repo:latest-{amd64,arm64}"
  docker tag "$avalanchego_dockerhub_repo:$COMMIT_HASH-amd64" "$avalanchego_dockerhub_repo:latest-amd64"
  docker tag "$avalanchego_dockerhub_repo:$COMMIT_HASH-arm64" "$avalanchego_dockerhub_repo:latest-arm64"
fi

## pushing image with tags
docker image push -a "$avalanchego_dockerhub_repo"

create_multi_arch_image() {
  local images=$1
  local tag=$2
  local repo="$avalanchego_dockerhub_repo"

  echo "Creating multi-arch Docker Image $repo:$tag"

  docker manifest create "$repo:$tag" ${images[@]}

  # Annotate the manifest with the OS and Architecture for each image
  for image in "${images[@]}"; do
    local arch=$(echo $image | rev | cut -d- -f1 | rev)
    docker manifest annotate "$repo:$tag" "$image" --os linux --arch $arch
  done

  docker manifest push "$repo:$tag"
}

# Create multi-arch images
IMAGES=("$avalanchego_dockerhub_repo:$COMMIT_HASH-amd64" "$avalanchego_dockerhub_repo:$COMMIT_HASH-arm64"
create_multi_arch_image "$IMAGES" "$COMMIT_HASH"
create_multi_arch_image "$IMAGES" "$current_branch"
if [[ $current_branch == "master" ]]; then
  create_multi_arch_image "$IMAGES" "latest"
fi
RACE_DETECTION_IMAGES=("$avalanchego_dockerhub_repo:$COMMIT_HASH-race-amd64" "$avalanchego_dockerhub_repo:$COMMIT_HASH-race-arm64")
create_multi_arch_image "$RACE_DETECTION_IMAGES" "$COMMIT_HASH-race"
create_multi_arch_image "$RACE_DETECTION_IMAGES" "$current_branch-race"
