#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Caminogo root folder
CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the versions
source "$CAMINO_PATH"/scripts/versions.sh
# Load the constants
source "$CAMINO_PATH"/scripts/constants.sh

# Download dependencies
echo "Downloading dependencies..."
go mod download

# Build caminogo
"$CAMINO_PATH"/scripts/build_camino.sh

# Build caminoethvm
"$CAMINO_PATH"/scripts/build_caminoethvm.sh

# Exit build successfully if the binaries are created
if [[ -f "$caminogo_path" && -f "$evm_path" ]]; then
        echo "Build Successful"
        exit 0
else
        echo "Build failure" >&2
        exit 1
fi
