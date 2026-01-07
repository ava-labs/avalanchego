#!/usr/bin/env bash

set -euo pipefail

# Run chaos testing against nodes deployed to a kind cluster with Chaos Mesh.
# This runs the antithesis workload with fault injection enabled.

if ! [[ "$0" =~ scripts/tests.chaos.kube.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Collect args for starting the cluster (kubeconfig, context, collectors)
START_CLUSTER_ARGS=("--install-chaos-mesh")
for arg in "$@"; do
  if [[ "${arg}" =~ "--kubeconfig=" || "${arg}" =~ "--kubeconfig-context=" || "${arg}" =~ "--start-metrics-collector" || "${arg}" =~ "--start-logs-collector" ]]; then
    START_CLUSTER_ARGS+=("${arg}")
  fi
done

echo "Starting kind cluster with Chaos Mesh: ${START_CLUSTER_ARGS[*]}"
./bin/tmpnetctl start-kind-cluster "${START_CLUSTER_ARGS[@]}"

# Use an image that will be pushed to the local registry that the kind cluster is configured to use.
AVALANCHEGO_IMAGE="localhost:5001/avalanchego"
if [[ -n "${SKIP_BUILD_IMAGE:-}" ]]; then
  echo "Skipping build of avalanchego image due to SKIP_BUILD_IMAGE=${SKIP_BUILD_IMAGE}"
else
  DOCKER_IMAGE="${AVALANCHEGO_IMAGE}" FORCE_TAG_MASTER=1 SKIP_BUILD_RACE=1 bash -x ./scripts/build_image.sh
fi

# Determine kubeconfig context to use
KUBECONFIG_CONTEXT=""
if [[ "$*" =~ --kubeconfig-context ]]; then
    echo "Using provided kubeconfig context from arguments"
else
    KUBECONFIG_CONTEXT="--kubeconfig-context=kind-kind-tmpnet"
    echo "Defaulting to limited-permission context 'kind-kind-tmpnet' to test RBAC Role permissions"
fi

# Default duration for chaos testing (can be overridden via --duration flag)
DURATION="${CHAOS_DURATION:-10m}"

echo "Running chaos tests with fault injection..."
go run ./tests/antithesis/avalanchego \
  --runtime=kube \
  --kube-image="${AVALANCHEGO_IMAGE}:master" \
  --inject-faults \
  --duration="${DURATION}" \
  ${KUBECONFIG_CONTEXT} \
  "$@"
