#!/usr/bin/env bash

set -euo pipefail

# Validates the construction of the antithesis images by:
#
#   1. Building the antithesis test images
#   2. Extracting the docker compose configuration from the config image
#   3. Running the workload and its target network without error for a minute
#   4. Stopping the workload and its target network
#

# e.g.,
# ./scripts/tests.build_antithesis_images.sh         # Test build of antithesis images
# DEBUG=1 ./scripts/tests.build_antithesis_images.sh # Retain the temporary compose path for troubleshooting

SUBNET_EVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Discover the default tag that will be used for the image
source "${SUBNET_EVM_PATH}"/scripts/constants.sh
export IMAGE_TAG="${DOCKERHUB_TAG}"

# Build the images
bash -x "${SUBNET_EVM_PATH}"/scripts/build_antithesis_images.sh

# Test the images
AVALANCHEGO_CLONE_PATH="${AVALANCHEGO_CLONE_PATH:-${SUBNET_EVM_PATH}/avalanchego}"
export IMAGE_NAME="antithesis-subnet-evm-config"
export DEBUG="${DEBUG:-}"
set -x
# shellcheck source=/dev/null
. "${AVALANCHEGO_CLONE_PATH}"/scripts/lib_test_antithesis_images.sh
