#!/usr/bin/env bash

# Run load test against nodes deployed to a kind cluster

if ! [[ "$0" =~ scripts/tests.load.kube.sh ]]; then
    echo "must be run from repository root"
    exit 255
fi

# This script will use kubeconfig arguments if supplied
./scripts/start_kind_cluster.sh "$@"

# Build AvalancheGo image
DOCKER_IMAGE=localhost:5001/avalanchego FORCE_TAG_LATEST=1 ./scripts/build_image.sh 

go run ./tests/load/c/main --runtime=kube "$@"
