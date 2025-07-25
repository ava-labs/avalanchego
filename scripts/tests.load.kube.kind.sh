#!/usr/bin/env bash

set -euo pipefail

# Run load test against nodes deployed to a kind cluster

if ! [[ "$0" =~ scripts/tests.load.kube.kind.sh ]]; then
    echo "must be run from repository root"
    exit 255
fi

# This script will use kubeconfig arguments if supplied
./scripts/start_kind_cluster.sh "$@"

# Build AvalancheGo image
AVALANCHEGO_IMAGE="localhost:5001/avalanchego"
if [[ -n "${SKIP_BUILD_IMAGE:-}" ]]; then
  echo "Skipping build of avalanchego image due to SKIP_BUILD_IMAGE=${SKIP_BUILD_IMAGE}"
else
  DOCKER_IMAGE="$AVALANCHEGO_IMAGE" FORCE_TAG_LATEST=1 ./scripts/build_image.sh
fi

# Determine kubeconfig context to use
KUBECONFIG_CONTEXT=""

# Check if --kubeconfig-context is already provided in arguments
if [[ "$*" =~ --kubeconfig-context ]]; then
    # User provided a context, use it as-is
    echo "Using provided kubeconfig context from arguments"
else
    # Default to the RBAC context
    KUBECONFIG_CONTEXT="--kubeconfig-context=kind-kind-tmpnet"
    echo "Defaulting to limited-permission context 'kind-kind-tmpnet' to test RBAC Role permissions"
fi

go run ./tests/load/main --runtime=kube --kube-image="$AVALANCHEGO_IMAGE" "$KUBECONFIG_CONTEXT" "$@"
