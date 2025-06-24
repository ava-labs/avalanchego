#!/usr/bin/env bash

set -euo pipefail

# Run load test against nodes deployed to a kind cluster

if ! [[ "$0" =~ scripts/tests.load.kube.sh ]]; then
    echo "must be run from repository root"
    exit 255
fi

# This script will use kubeconfig arguments if supplied
./scripts/start_kind_cluster.sh "$@"

# Build AvalancheGo image
AVALANCHEGO_IMAGE="localhost:5001/avalanchego"
DOCKER_IMAGE="$AVALANCHEGO_IMAGE" FORCE_TAG_LATEST=1 ./scripts/build_image.sh 

go run ./tests/load/c/main --runtime=kube --kube-image="$AVALANCHEGO_IMAGE" "$@"
