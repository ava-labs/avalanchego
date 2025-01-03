#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_xsvm_image.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# TODO(marun) This image name should be configurable
DOCKER_IMAGE="localhost:5001/avalanchego"

# Build the avalancehgo node image
FORCE_TAG_LATEST=1 SKIP_BUILD_RACE=1 DOCKER_IMAGE="${DOCKER_IMAGE}" ./scripts/build_image.sh

# TODO(marun) conditionally push the image to the registry
GO_VERSION="$(go list -m -f '{{.GoVersion}}')"
docker buildx build --build-arg GO_VERSION="${GO_VERSION}" --build-arg AVALANCHEGO_NODE_IMAGE="${DOCKER_IMAGE}" \
       --push -t "${DOCKER_IMAGE}-xsvm" -f "${AVALANCHE_PATH}/vms/example/xsvm/Dockerfile" .
