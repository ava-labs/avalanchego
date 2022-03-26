#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanchego root folder
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the versions
source "$CAMINO_PATH"/scripts/versions.sh
# Load the constants
source "$CAMINO_PATH"/scripts/constants.sh

# build_image_from_remote.sh is deprecated
source "$CAMINO_PATH"/scripts/build_local_image.sh
