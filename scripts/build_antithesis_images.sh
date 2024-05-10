#!/usr/bin/env bash

set -euo pipefail

# Builds docker images for antithesis testing.

# e.g.,
# TEST_SETUP=avalanchego ./scripts/build_antithesis_images.sh                                    # Build local images for avalanchego
# TEST_SETUP=xsvm ./scripts/build_antithesis_images.sh                                           # Build local images for xsvm
# TEST_SETUP=xsvm IMAGE_PREFIX=<registry>/<repo> TAG=latest ./scripts/build_antithesis_images.sh # Specify a prefix to enable image push and use a specific tag

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Specifying an image prefix will ensure the image is pushed after build
IMAGE_PREFIX="${IMAGE_PREFIX:-}"

TAG="${TAG:-}"
if [[ -z "${TAG}" ]]; then
  # Default to tagging with the commit hash
  source "${AVALANCHE_PATH}"/scripts/constants.sh
  TAG="${commit_hash}"
fi

# The dockerfiles don't specify the golang version to minimize the changes required to bump
# the version. Instead, the golang version is provided as an argument.
GO_VERSION="$(go list -m -f '{{.GoVersion}}')"

function build_images {
  local test_setup=$1
  local uninstrumented_node_dockerfile=$2
  local node_only=${3:-}

  # Define image names
  local base_image_name="antithesis-${test_setup}"
  local avalanchego_node_image_name="antithesis-avalanchego-node:${TAG}"
  if [[ -n "${IMAGE_PREFIX}" ]]; then
    base_image_name="${IMAGE_PREFIX}/${base_image_name}"
    avalanchego_node_image_name="${IMAGE_PREFIX}/${avalanchego_node_image_name}"
  fi
  local node_image_name="${base_image_name}-node:${TAG}"
  local workload_image_name="${base_image_name}-workload:${TAG}"
  local config_image_name="${base_image_name}-config:${TAG}"

  # Define dockerfiles
  local base_dockerfile="${AVALANCHE_PATH}/tests/antithesis/${test_setup}/Dockerfile"
  local node_dockerfile="${base_dockerfile}.node"
  if [[ "$(go env GOARCH)" == "arm64" ]]; then
    # Antithesis instrumentation is only supported on amd64. On apple silicon (arm64), the
    # uninstrumented Dockerfile will be used to build the node image to enable local test
    # development.
    node_dockerfile="${uninstrumented_node_dockerfile}"
  fi

  # Define default build command
  local docker_cmd="docker buildx build --build-arg GO_VERSION=${GO_VERSION} --build-arg NODE_IMAGE=${node_image_name}"

  if [[ "${test_setup}" == "xsvm" ]]; then
    # The xsvm node image is built on the avalanchego node image
    docker_cmd="${docker_cmd} --build-arg AVALANCHEGO_NODE_IMAGE=${avalanchego_node_image_name}"
  fi

  # Build node image first to allow the config and workload image builds to use it.
  ${docker_cmd} -t "${node_image_name}" -f "${node_dockerfile}" "${AVALANCHE_PATH}"

  if [[ -z "${node_only}" || "${node_only}" == "0" ]]; then
    # Skip building the config and workload images. Supports building the avalanchego
    # node image as the base image for the xsvm node image.
    ${docker_cmd} --build-arg IMAGE_TAG="${TAG}" -t "${config_image_name}" -f "${base_dockerfile}.config" "${AVALANCHE_PATH}"
    ${docker_cmd} -t "${workload_image_name}" -f "${base_dockerfile}.workload" "${AVALANCHE_PATH}"
  fi
}

TEST_SETUP="${TEST_SETUP:-}"
if [[ "${TEST_SETUP}" == "avalanchego" ]]; then
  build_images avalanchego "${AVALANCHE_PATH}/Dockerfile"
elif [[ "${TEST_SETUP}" == "xsvm" ]]; then
  # Only build the node image to use as the base for the xsvm image
  NODE_ONLY=1
  build_images avalanchego "${AVALANCHE_PATH}/Dockerfile" "${NODE_ONLY}"

  build_images xsvm "${AVALANCHE_PATH}/vms/example/xsvm/Dockerfile"
else
  echo "TEST_SETUP must be set. Valid values are 'avalanchego' or 'xsvm'"
  exit 255
fi
