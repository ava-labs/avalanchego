#!/usr/bin/env bash

set -euo pipefail

# Sanity check the image build by attempting to build and run the image without error.

# Directory above this script
SUBNET_EVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

# Build a local image
"${SUBNET_EVM_PATH}"/scripts/build_docker_image.sh

# Check that the image can be run and contains the plugin
echo "Checking version of the plugin provided by the image"
docker run -t --rm "${DOCKERHUB_REPO}:${DOCKERHUB_TAG}" /avalanchego/build/plugins/"${DEFAULT_VM_ID}" --version
echo "" # --version output doesn't include a newline
echo "Successfully checked image build"
