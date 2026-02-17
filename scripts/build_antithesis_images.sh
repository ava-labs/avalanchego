#!/usr/bin/env bash

set -euo pipefail

# Builds docker images for antithesis testing.

# e.g.,
# TEST_SETUP=avalanchego ./scripts/build_antithesis_images.sh                                          # Build local images for avalanchego
# TEST_SETUP=avalanchego NODE_ONLY=1 ./scripts/build_antithesis_images.sh                              # Build only a local node image for avalanchego
# TEST_SETUP=xsvm ./scripts/build_antithesis_images.sh                                                 # Build local images for xsvm
# TEST_SETUP=xsvm IMAGE_PREFIX=<registry>/<repo> IMAGE_TAG=latest ./scripts/build_antithesis_images.sh # Specify a prefix to enable image push and use a specific tag

TEST_SETUP="${TEST_SETUP:-}"
if [[ "${TEST_SETUP}" != "avalanchego" && "${TEST_SETUP}" != "xsvm" && "${TEST_SETUP}" != "subnet-evm" ]]; then
  echo "TEST_SETUP must be set. Valid values are 'avalanchego', 'xsvm', or 'subnet-evm'"
  exit 255
fi

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

source "${AVALANCHE_PATH}"/scripts/constants.sh
source "${AVALANCHE_PATH}"/scripts/git_commit.sh

# Import common functions used to build images for antithesis test setups
source "${AVALANCHE_PATH}"/scripts/lib_build_antithesis_images.sh

# Specifying an image prefix will ensure the image is pushed after build
IMAGE_PREFIX="${IMAGE_PREFIX:-}"

IMAGE_TAG="${IMAGE_TAG:-}"
if [[ -z "${IMAGE_TAG}" ]]; then
  # Default to tagging with the commit hash
  IMAGE_TAG="${commit_hash}"
fi

# The dockerfiles don't specify the golang version to minimize the changes required to bump
# the version. Instead, the golang version is provided as an argument. Use head -1 because
# go workspaces list multiple modules; CI validates all modules use the same Go version.
GO_VERSION="$(go list -m -f '{{.GoVersion}}' | head -1)"

# Helper to simplify calling build_builder_image for test setups in this repo
function build_builder_image_for_avalanchego {
  echo "Building builder image"
  build_antithesis_builder_image "${GO_VERSION}" "antithesis-avalanchego-builder:${IMAGE_TAG}" "${AVALANCHE_PATH}" "${AVALANCHE_PATH}"
}

# Helper to simplify calling build_antithesis_images for test setups in this repo
function build_antithesis_images_for_avalanchego {
  local test_setup=$1
  local image_prefix=$2
  local uninstrumented_node_dockerfile=$3
  local node_only=${4:-}

  if [[ -z "${node_only}" ]]; then
    echo "Building node image for ${test_setup}"
  else
    echo "Building images for ${test_setup}"
  fi
  build_antithesis_images "${GO_VERSION}" "${image_prefix}" "antithesis-${test_setup}" "${IMAGE_TAG}" "${IMAGE_TAG}" \
                          "${AVALANCHE_PATH}/tests/antithesis/${test_setup}/Dockerfile" "${uninstrumented_node_dockerfile}" \
                          "${AVALANCHE_PATH}" "${node_only}" "${git_commit}"
}

if [[ "${TEST_SETUP}" == "avalanchego" ]]; then
  build_builder_image_for_avalanchego

  echo "Generating compose configuration for ${TEST_SETUP}"
  gen_antithesis_compose_config "${IMAGE_TAG}" "${AVALANCHE_PATH}/tests/antithesis/avalanchego/gencomposeconfig" \
                                "${AVALANCHE_PATH}/build/antithesis/avalanchego"

  build_antithesis_images_for_avalanchego "${TEST_SETUP}" "${IMAGE_PREFIX}" "${AVALANCHE_PATH}/Dockerfile" "${NODE_ONLY:-}"
else
  # VM test setups (xsvm, subnet-evm) follow a common pattern
  build_builder_image_for_avalanchego

  # Build VM-specific builder if needed
  if [[ "${TEST_SETUP}" == "subnet-evm" ]]; then
    echo "Building subnet-evm builder image"
    build_antithesis_builder_image "${GO_VERSION}" "antithesis-subnet-evm-builder:${IMAGE_TAG}" "${AVALANCHE_PATH}" "${AVALANCHE_PATH}"
  fi

  # Only build the avalanchego node image to use as the base for the VM image. Provide an empty
  # image prefix (the 1st argument) to prevent the image from being pushed
  NODE_ONLY=1
  build_antithesis_images_for_avalanchego avalanchego "" "${AVALANCHE_PATH}/Dockerfile" "${NODE_ONLY}"

  # Build required binaries for the VM test setup
  echo "Building binaries required for configuring the ${TEST_SETUP} test setup"
  "${AVALANCHE_PATH}"/scripts/build.sh
  if [[ "${TEST_SETUP}" == "xsvm" ]]; then
    "${AVALANCHE_PATH}"/scripts/build_xsvm.sh
  elif [[ "${TEST_SETUP}" == "subnet-evm" ]]; then
    "${AVALANCHE_PATH}"/graft/subnet-evm/scripts/build.sh
  fi

  # Set VM-specific paths
  if [[ "${TEST_SETUP}" == "xsvm" ]]; then
    vm_dockerfile="${AVALANCHE_PATH}/vms/example/xsvm/Dockerfile"
  elif [[ "${TEST_SETUP}" == "subnet-evm" ]]; then
    vm_dockerfile="${AVALANCHE_PATH}/graft/subnet-evm/Dockerfile"
  fi

  echo "Generating compose configuration for ${TEST_SETUP}"
  if [[ "${TEST_SETUP}" == "xsvm" ]]; then
    gencomposeconfig_path="${AVALANCHE_PATH}/tests/antithesis/xsvm/gencomposeconfig"
  elif [[ "${TEST_SETUP}" == "subnet-evm" ]]; then
    gencomposeconfig_path="${AVALANCHE_PATH}/graft/subnet-evm/tests/antithesis/gencomposeconfig"
  fi
  gen_antithesis_compose_config "${IMAGE_TAG}" \
    "${gencomposeconfig_path}" \
    "${AVALANCHE_PATH}/build/antithesis/${TEST_SETUP}" \
    "AVALANCHEGO_PATH=${AVALANCHE_PATH}/build/avalanchego AVAGO_PLUGIN_DIR=${AVALANCHE_PATH}/build/plugins"

  build_antithesis_images_for_avalanchego "${TEST_SETUP}" "${IMAGE_PREFIX}" "${vm_dockerfile}"
fi
