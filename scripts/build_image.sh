#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanchego root folder
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the versions
source "$AVALANCHE_PATH"/scripts/versions.sh
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

source "$AVALANCHE_PATH"/scripts/build_local_image.sh
