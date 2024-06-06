#!/usr/bin/env bash

set -euo pipefail

# Builds docker images for antithesis testing.

# e.g.,
# TEST_SETUP=avalanchego ./scripts/build_antithesis_images.sh                                    # Build local images for avalanchego
# TEST_SETUP=avalanchego NODE_ONLY=1 ./scripts/build_antithesis_images.sh                        # Build only a local node image for avalanchego
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
  local image_prefix=$3
  local node_only=${4:-}

  # Define image names
  local base_image_name="antithesis-${test_setup}"
  if [[ -n "${image_prefix}" ]]; then
    base_image_name="${image_prefix}/${base_image_name}"
  fi
  local node_image_name="${base_image_name}-node:${TAG}"
  local workload_image_name="${base_image_name}-workload:${TAG}"
  local config_image_name="${base_image_name}-config:${TAG}"
  # The same builder image is used to build node and workload images for all test
  # setups. It is not intended to be pushed.
  local builder_image_name="antithesis-avalanchego-builder:${TAG}"

  # Define dockerfiles
  local base_dockerfile="${AVALANCHE_PATH}/tests/antithesis/${test_setup}/Dockerfile"
  local builder_dockerfile="${base_dockerfile}.builder-instrumented"
  local node_dockerfile="${base_dockerfile}.node"
  # Working directory for instrumented builds
  local builder_workdir="/avalanchego_instrumented/customer"
  if [[ "$(go env GOARCH)" == "arm64" ]]; then
    # Antithesis instrumentation is only supported on amd64. On apple silicon (arm64),
    # uninstrumented Dockerfiles will be used to enable local test development.
    builder_dockerfile="${base_dockerfile}.builder-uninstrumented"
    node_dockerfile="${uninstrumented_node_dockerfile}"
    # Working directory for uninstrumented builds
    builder_workdir="/build"
  fi

  # Define default build command
  local docker_cmd="docker buildx build\
 --build-arg GO_VERSION=${GO_VERSION}\
 --build-arg NODE_IMAGE=${node_image_name}\
 --build-arg BUILDER_IMAGE=${builder_image_name}\
 --build-arg BUILDER_WORKDIR=${builder_workdir}\
 --build-arg TAG=${TAG}"

  if [[ "${test_setup}" == "xsvm" ]]; then
    # The xsvm node image is built on the avalanchego node image, which is assumed to have already been
    # built. The image name doesn't include the image prefix because it is not intended to be pushed.
    docker_cmd="${docker_cmd} --build-arg AVALANCHEGO_NODE_IMAGE=antithesis-avalanchego-node:${TAG}"
  fi

  if [[ "${test_setup}" == "avalanchego" ]]; then
    # Build the image that enables compiling golang binaries for the node and workload
    # image builds. The builder image is intended to enable building instrumented binaries
    # if built on amd64 and non-instrumented binaries if built on arm64.
    #
    # The builder image is not intended to be pushed so it needs to be built in advance of
    # adding `--push` to docker_cmd. Since it is never prefixed with `[registry]/[repo]`,
    # attempting to push will result in an error like `unauthorized: access token has
    # insufficient scopes`.
    ${docker_cmd} -t "${builder_image_name}" -f "${builder_dockerfile}" "${AVALANCHE_PATH}"
  fi

  if [[ -n "${image_prefix}" && -z "${node_only}" ]]; then
    # Push images with an image prefix since the prefix defines a
    # registry location, and only if building all images. When
    # building just the node image the image is only intended to be
    # used locally.
    docker_cmd="${docker_cmd} --push"
  fi

  # Build node image first to allow the workload image to use it.
  ${docker_cmd} -t "${node_image_name}" -f "${node_dockerfile}" "${AVALANCHE_PATH}"

  if [[ -n "${node_only}" ]]; then
    # Skip building the config and workload images. Supports building the avalanchego
    # node image as the base image for the xsvm node image.
    return
  fi

  TARGET_PATH="${AVALANCHE_PATH}/build/antithesis/${test_setup}"
  if [[ -d "${TARGET_PATH}" ]]; then
    # Ensure the target path is empty before generating the compose config
    rm -r "${TARGET_PATH:?}"
  fi

  # Define the env vars for the compose config generation
  COMPOSE_ENV="TARGET_PATH=${TARGET_PATH} IMAGE_TAG=${TAG}"

  if [[ "${test_setup}" == "xsvm" ]]; then
    # Ensure avalanchego and xsvm binaries are available to create an initial db state that includes subnets.
    "${AVALANCHE_PATH}"/scripts/build.sh
    "${AVALANCHE_PATH}"/scripts/build_xsvm.sh
    COMPOSE_ENV="${COMPOSE_ENV} AVALANCHEGO_PATH=${AVALANCHE_PATH}/build/avalanchego AVALANCHEGO_PLUGIN_DIR=${HOME}/.avalanchego/plugins"
  fi

  # Generate compose config for copying into the config image
  # shellcheck disable=SC2086
  env ${COMPOSE_ENV} go run "${AVALANCHE_PATH}/tests/antithesis/${test_setup}/gencomposeconfig"

  # Build the config image
  ${docker_cmd} -t "${config_image_name}" -f "${base_dockerfile}.config" "${AVALANCHE_PATH}"

  # Build the workload image
  ${docker_cmd} -t "${workload_image_name}" -f "${base_dockerfile}.workload" "${AVALANCHE_PATH}"
}

TEST_SETUP="${TEST_SETUP:-}"
if [[ "${TEST_SETUP}" == "avalanchego" ]]; then
  build_images avalanchego "${AVALANCHE_PATH}/Dockerfile" "${IMAGE_PREFIX}" "${NODE_ONLY:-}"
elif [[ "${TEST_SETUP}" == "xsvm" ]]; then
  # Only build the node image to use as the base for the xsvm image. Provide an empty
  # image prefix (the 3rd argument) to prevent the image from being pushed
  NODE_ONLY=1
  build_images avalanchego "${AVALANCHE_PATH}/Dockerfile" "" "${NODE_ONLY}"

  build_images xsvm "${AVALANCHE_PATH}/vms/example/xsvm/Dockerfile" "${IMAGE_PREFIX}"
else
  echo "TEST_SETUP must be set. Valid values are 'avalanchego' or 'xsvm'"
  exit 255
fi
