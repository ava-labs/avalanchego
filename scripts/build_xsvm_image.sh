#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/build_image.sh                                                   # Build local single-arch image
# AVALANCHEGO_IMAGE=localhost:5001/avalanchego ./scripts/build_xsvm_image.sh # Build and push image to private registry

if ! [[ "$0" =~ scripts/build_xsvm_image.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/image_tag.sh

AVALANCHEGO_IMAGE="${AVALANCHEGO_IMAGE:-avalanchego}"
XSVM_IMAGE="${XSVM_IMAGE:-avalanchego-xsvm}"

# Build the avalanchego base image
SKIP_BUILD_RACE=1 DOCKER_IMAGE="${AVALANCHEGO_IMAGE}" bash -x ./scripts/build_image.sh

DOCKER_CMD=("docker" "buildx" "build")
if [[ "${XSVM_IMAGE}" == *"/"* ]]; then
  # Push to a registry when the image name includes a slash which indicates the
  # use of a registry e.g.
  #
  #  - dockerhub: [repo]/[image name]:[tag]
  #  - private registry: [private registry hostname]/[image name]:[tag]
  DOCKER_CMD+=("--push")
fi

GO_VERSION="$(go list -m -f '{{.GoVersion}}')"

"${DOCKER_CMD[@]}" --build-arg GO_VERSION="${GO_VERSION}" --build-arg AVALANCHEGO_NODE_IMAGE="${AVALANCHEGO_IMAGE}:${image_tag}" \
  -t "${XSVM_IMAGE}" -f ./vms/example/xsvm/Dockerfile .
