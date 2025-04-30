#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/build_bootstrap_monitor_image.sh                                            # Build local image
# DOCKER_IMAGE=my-bootstrap-monitor ./scripts/build_bootstrap_monitor_image.sh          # Build local single arch image with a custom image name
# DOCKER_IMAGE=avaplatform/bootstrap-monitor ./scripts/build_bootstrap_monitor_image.sh # Build and push image to docker hub

# Builds the image for the bootstrap monitor

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# The published name should be 'avaplatform/bootstrap-monitor', but to avoid unintentional pushes it
# is defaulted to 'bootstrap-monitor' (without a repo or registry name) which can only be used to
# create local images.
export DOCKER_IMAGE=${DOCKER_IMAGE:-"bootstrap-monitor"}

# Skip building the race image
export SKIP_BUILD_RACE=1

# Reuse the avalanchego build script for convenience. The image will have a CMD of "./avalanchego", so
# to run the bootstrap monitor will need to specify ./bootstrap-monitor".
#
# TODO(marun) Figure out how to set the CMD for a multi-arch image.
bash -x "${AVALANCHE_PATH}"/scripts/build_image.sh --build-arg BUILD_SCRIPT=build_bootstrap_monitor.sh
