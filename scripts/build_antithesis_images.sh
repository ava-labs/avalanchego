#!/usr/bin/env bash

set -euo pipefail

# Builds docker images for antithesis testing.

# e.g.,
# ./scripts/build_antithesis_images.sh                                           # Build local images
# IMAGE_PREFIX=<registry>/<repo> TAG=latest ./scripts/build_antithesis_images.sh # Specify a prefix to enable image push and use a specific tag

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

  # Define image names
  local base_image_name="antithesis-${test_setup}"
  if [[ -n "${IMAGE_PREFIX}" ]]; then
    base_image_name="${IMAGE_PREFIX}/${base_image_name}"
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
  local docker_cmd="docker buildx build --build-arg GO_VERSION=${GO_VERSION}"

  # Build node image first to allow the config and workload image builds to use it.
  ${docker_cmd} -t "${node_image_name}" -f "${node_dockerfile}" "${AVALANCHE_PATH}"
  ${docker_cmd} --build-arg NODE_IMAGE="${node_image_name}" -t "${workload_image_name}" -f "${base_dockerfile}.workload" "${AVALANCHE_PATH}"
  ${docker_cmd} --build-arg IMAGE_TAG="${TAG}" -t "${config_image_name}" -f "${base_dockerfile}.config" "${AVALANCHE_PATH}"
}

TEST_SETUP="${TEST_SETUP:-}"
if [[ "${TEST_SETUP}" == "avalanchego" ]]; then
  build_images avalanchego "${AVALANCHE_PATH}/Dockerfile"
else
  echo "TEST_SETUP must be set. Valid values are 'avalanchego'"
  exit 255
fi
