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
# shellcheck source=graft/subnet-evm/scripts/constants.sh disable=SC1091
source "${SUBNET_EVM_PATH}"/scripts/constants.sh
# shellcheck disable=SC2154
export IMAGE_TAG="${commit_hash}"

# Build the images
bash -x "${SUBNET_EVM_PATH}"/scripts/build_antithesis_images.sh

# Test the images
AVALANCHE_PATH="${SUBNET_EVM_PATH}/../.."
export IMAGE_NAME="antithesis-subnet-evm-config"
export DEBUG="${DEBUG:-}"
set -x
# shellcheck source=/dev/null
. "${AVALANCHE_PATH}"/scripts/lib_test_antithesis_images.sh
