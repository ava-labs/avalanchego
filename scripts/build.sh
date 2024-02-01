#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Camino-Node root folder
CAMINO_NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

echo "Downloading dependencies..."
(cd $CAMINO_NODE_PATH && go mod download)

# Build caminogo
"$CAMINO_NODE_PATH"/scripts/build_camino.sh

CAMINO_NETWORK_RUNNER_PATH="$CAMINO_NODE_PATH"/tools/camino-network-runner

if [ ! -f $CAMINO_NETWORK_RUNNER_PATH/.git ]; then
    echo "Initializing git submodules..."
    git --git-dir $CAMINO_NODE_PATH/.git submodule update --init --recursive
fi

# Build camino-network-runner
"$CAMINO_NETWORK_RUNNER_PATH"/scripts/build.sh