#!/usr/bin/env bash

set -euo pipefail

# Validates the construction of the subnet-evm antithesis images

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Discover the default tag
source "${AVALANCHE_PATH}"/scripts/git_commit.sh
export IMAGE_TAG="${commit_hash}"

# Build the images
export TEST_SETUP="subnet-evm"
bash -x "${AVALANCHE_PATH}"/scripts/build_antithesis_images.sh

# Test the images
export IMAGE_NAME="antithesis-subnet-evm-config"
export DEBUG="${DEBUG:-}"
set -x
. "${AVALANCHE_PATH}"/scripts/lib_test_antithesis_images.sh
