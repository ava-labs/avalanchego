#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/tests.topology.kube.kind.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

./scripts/start_kind_cluster.sh --install-chaos-mesh "$@"

AVALANCHEGO_IMAGE="localhost:5001/avalanchego"
if [[ -n "${SKIP_BUILD_IMAGE:-}" ]]; then
  echo "Skipping build of avalanchego image due to SKIP_BUILD_IMAGE=${SKIP_BUILD_IMAGE}"
else
  DOCKER_IMAGE="$AVALANCHEGO_IMAGE" FORCE_TAG_MASTER=1 ./scripts/build_image.sh
fi

RUNTIME_CONTEXT_ARGS=()
if [[ "$*" =~ --kubeconfig-context ]]; then
  echo "Using provided runtime kubeconfig context from arguments"
else
  RUNTIME_CONTEXT_ARGS+=(--kubeconfig-context=kind-kind-tmpnet)
  echo "Defaulting runtime kubeconfig context to limited-permission context 'kind-kind-tmpnet' to test RBAC permissions"
fi

ADMIN_CONTEXT_ARGS=()
if [[ "$*" =~ --admin-kubeconfig-context ]]; then
  echo "Using provided admin kubeconfig context from arguments"
else
  ADMIN_CONTEXT_ARGS+=(--admin-kubeconfig-context=kind-kind)
  echo "Defaulting admin kubeconfig context to cluster-admin context 'kind-kind' for verification"
fi

./bin/ginkgo -v ./tests/fixture/tmpnet/e2e -- \
  --runtime=kube \
  "--kube-image=$AVALANCHEGO_IMAGE:master" \
  --node-count=2 \
  --topology-latency=1s \
  "${RUNTIME_CONTEXT_ARGS[@]}" \
  "${ADMIN_CONTEXT_ARGS[@]}" \
  "$@"
