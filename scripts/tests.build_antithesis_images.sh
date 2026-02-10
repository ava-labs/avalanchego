#!/usr/bin/env bash

set -euo pipefail

# Validates the construction of the antithesis images for a test setup specified by TEST_SETUP by:
#
#   1. Building the antithesis test image
#   2. Extracting the docker compose configuration from the image
#   3. Running the workload and its target network without error for a minute
#   4. Stopping the workload and its target network
#
# `docker compose` is used (docker compose v2 plugin) due to it being installed by default on
# public github runners. `docker-compose` (the v1 plugin) is not installed by default.

# e.g.,
# TEST_SETUP=avalanchego ./scripts/tests.build_antithesis_images.sh         # Test build of images for avalanchego test setup
# DEBUG=1 TEST_SETUP=avalanchego ./scripts/tests.build_antithesis_images.sh # Retain the temporary compose path for troubleshooting

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Discover the default tag that will be used for the image
source "${AVALANCHE_PATH}"/scripts/vcs.sh
export IMAGE_TAG="${vcs_commit_short}"

# Build the images for the specified test setup
export TEST_SETUP="${TEST_SETUP:-}"
bash -x "${AVALANCHE_PATH}"/scripts/build_antithesis_images.sh

# Test the images
export IMAGE_NAME="antithesis-${TEST_SETUP}-config"
export DEBUG="${DEBUG:-}"
set -x
. "${AVALANCHE_PATH}"/scripts/lib_test_antithesis_images.sh
