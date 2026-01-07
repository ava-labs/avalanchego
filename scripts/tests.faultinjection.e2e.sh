#!/usr/bin/env bash

set -euo pipefail

# Run fault injection e2e tests against nodes deployed to a kind cluster with Chaos Mesh.

if ! [[ "$0" =~ scripts/tests.faultinjection.e2e.sh ]]; then
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
    KUBECONFIG_CONTEXT="--kubeconfig-context=kind-kind"
    echo "Defaulting to 'kind-kind' context"
fi

echo "Running fault injection e2e tests..."
./bin/ginkgo -v \
  --timeout=30m \
  ./tests/fixture/faultinjection/e2e/ \
  -- \
  --kube-image="${AVALANCHEGO_IMAGE}:master" \
  ${KUBECONFIG_CONTEXT} \
  "$@"
