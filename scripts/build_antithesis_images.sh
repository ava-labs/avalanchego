#!/usr/bin/env bash

set -euo pipefail

# Builds docker images for antithesis testing.

# e.g.,
# ./scripts/build_antithesis_images.sh                                                 # Build local images
# IMAGE_PREFIX=<registry>/<repo> IMAGE_TAG=latest ./scripts/build_antithesis_images.sh # Specify a prefix to enable image push and use a specific tag

# Directory above this script
SUBNET_EVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Assume it's necessary to build the avalanchego node image from source
# TODO(marun) Support use of a released node image if using a release version of avalanchego

source "${SUBNET_EVM_PATH}"/scripts/constants.sh
source "${SUBNET_EVM_PATH}"/scripts/lib_avalanchego_clone.sh

clone_avalanchego "${AVALANCHE_VERSION}"
AVALANCHEGO_IMAGE_TAG="$(avalanchego_image_tag_from_clone)"

# Build avalanchego node image in the clone path
pushd "${AVALANCHEGO_CLONE_PATH}" > /dev/null
  NODE_ONLY=1 TEST_SETUP=avalanchego IMAGE_TAG="${AVALANCHEGO_IMAGE_TAG}" bash -x "${AVALANCHEGO_CLONE_PATH}"/scripts/build_antithesis_images.sh
popd > /dev/null

# Specifying an image prefix will ensure the image is pushed after build
IMAGE_PREFIX="${IMAGE_PREFIX:-}"

IMAGE_TAG="${IMAGE_TAG:-}"
if [[ -z "${IMAGE_TAG}" ]]; then
  # Default to tagging with the commit hash
  source "${SUBNET_EVM_PATH}"/scripts/constants.sh
  IMAGE_TAG="${SUBNET_EVM_COMMIT::8}"
fi

# The dockerfiles don't specify the golang version to minimize the changes required to bump
# the version. Instead, the golang version is provided as an argument.
GO_VERSION="$(go list -m -f '{{.GoVersion}}')"

# Import common functions used to build images for antithesis test setups
# shellcheck source=/dev/null
source "${AVALANCHEGO_CLONE_PATH}"/scripts/lib_build_antithesis_images.sh

build_antithesis_builder_image "${GO_VERSION}" "antithesis-subnet-evm-builder:${IMAGE_TAG}" "${AVALANCHEGO_CLONE_PATH}" "${SUBNET_EVM_PATH}"

# Ensure avalanchego and subnet-evm binaries are available to create an initial db state that includes subnets.
pushd "${AVALANCHEGO_CLONE_PATH}" && ./scripts/build.sh && popd
"${SUBNET_EVM_PATH}"/scripts/build.sh

echo "Generating compose configuration"
gen_antithesis_compose_config "${IMAGE_TAG}" "${SUBNET_EVM_PATH}/tests/antithesis/gencomposeconfig" \
                              "${SUBNET_EVM_PATH}/build/antithesis" \
                              "AVALANCHEGO_PATH=${AVALANCHEGO_CLONE_PATH}/build/avalanchego \
                              AVAGO_PLUGIN_DIR=${DEFAULT_PLUGIN_DIR}"

build_antithesis_images "${GO_VERSION}" "${IMAGE_PREFIX}" "antithesis-subnet-evm" "${IMAGE_TAG}" \
                        "${AVALANCHEGO_IMAGE_TAG}" "${SUBNET_EVM_PATH}/tests/antithesis/Dockerfile" \
                        "${SUBNET_EVM_PATH}/Dockerfile" "${SUBNET_EVM_PATH}"
