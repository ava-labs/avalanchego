#!/usr/bin/env bash

set -euo pipefail

# Run load test against nodes deployed to a kubernetes cluster
#
# Usage:
#   # Run against local Kind cluster (default)
#   # Builds image from local code and uses local docker registry
#   ./scripts/tests.load.kube.sh
#
#   # Run with custom image against local Kind cluster
#   ./scripts/tests.load.kube.sh --kube-image=my-registry/avalanchego:v1.2.3
#
#   # Run against existing/remote cluster
#   # Uses avaplatform/avalanchego:latest from Docker Hub if --kube-image not specified
#   SKIP_KIND_CLUSTER=1 ./scripts/tests.load.kube.sh
#
#   # Run against remote cluster with custom image
#   SKIP_KIND_CLUSTER=1 ./scripts/tests.load.kube.sh --kube-image=avaplatform/avalanchego:092cf182-r
#
# Environment Variables:
#   SKIP_KIND_CLUSTER=1    Skip creating Kind cluster, use existing (local or remote) cluster
#
# The script forwards all arguments to the Go load test program.

if ! [[ "$0" =~ scripts/tests.load.kube.sh ]]; then
    echo "must be run from repository root"
    exit 255
fi

# Check if custom image is provided via arguments first
CUSTOM_IMAGE=""
if [[ "$*" =~ --kube-image=([^[:space:]]+) ]]; then
    CUSTOM_IMAGE="${BASH_REMATCH[1]}"
fi

if [[ -z "${SKIP_KIND_CLUSTER:-}" ]]; then
    echo "Starting Kind cluster..."
    ./scripts/start_kind_cluster.sh "$@"

    if [[ -n "$CUSTOM_IMAGE" ]]; then
        # Use custom image with Kind cluster
        AVALANCHEGO_IMAGE="$CUSTOM_IMAGE"
        echo "Using custom image with Kind cluster: $AVALANCHEGO_IMAGE"
    else
        # Build image from local code for Kind cluster
        AVALANCHEGO_IMAGE="localhost:5001/avalanchego"
        echo "Building AvalancheGo Docker image from local code: $AVALANCHEGO_IMAGE"
        DOCKER_IMAGE="$AVALANCHEGO_IMAGE" FORCE_TAG_LATEST=1 ./scripts/build_image.sh
    fi
else
    if [[ -n "$CUSTOM_IMAGE" ]]; then
        # Use custom image with remote cluster
        AVALANCHEGO_IMAGE="$CUSTOM_IMAGE"
        echo "Using custom image with remote cluster: $AVALANCHEGO_IMAGE"
    else
        # Use default Docker Hub image for remote cluster
        AVALANCHEGO_IMAGE="avaplatform/avalanchego:latest"
        echo "Using default Docker Hub image: $AVALANCHEGO_IMAGE"
    fi
fi

# Determine kubeconfig context to use
KUBECONFIG_CONTEXT=""

# Check if --kubeconfig-context is already provided in arguments
if [[ "$*" =~ --kubeconfig-context ]]; then
    echo "Using provided kubeconfig context from arguments"
elif [[ -z "${SKIP_KIND_CLUSTER:-}" ]]; then
    # Only default to kind context if using kind cluster
    KUBECONFIG_CONTEXT="--kubeconfig-context=kind-kind-tmpnet"
    echo "Defaulting 'kind-kind-tmpnet' context"
fi

go run ./tests/load/c/main --runtime=kube --kube-image="$AVALANCHEGO_IMAGE" "$KUBECONFIG_CONTEXT" "$@"
