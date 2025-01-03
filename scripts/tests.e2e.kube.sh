#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests for bootstrap monitor.

if ! [[ "$0" =~ scripts/tests.e2e.kube.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

./scripts/start_kind_cluster.sh

# TODO(marun) Make the namespace configurable
NAMESPACE=tmpnet
PATH="${PWD}/bin:$PATH" kubectl create namespace "${NAMESPACE}" || true

bash -x ./scripts/build_xsvm_image.sh

# Deploy promtail if loki credentails are provided
if [[ -n "${LOKI_USERNAME:-}" && -n "${LOKI_PASSWORD:-}" ]]; then
  PROMTAIL_NAMESPACE=default
  kubectl --namespace="${PROMTAIL_NAMESPACE}" create secret generic loki-credentials \
    --from-literal=LOKI_USERNAME="${LOKI_USERNAME}" \
    --from-literal=LOKI_PASSWORD="${LOKI_PASSWORD}"
  kubectl --namespace="${PROMTAIL_NAMESPACE}" apply -f ./tests/fixture/tmpnet/yaml/promtail-daemonset.yaml
else
  echo "LOKI_USERNAME and LOKI_PASSWORD not set, skipping promtail deployment"
fi

# TODO(marun) Is the path still necessary?
E2E_SERIAL=1 KUBECONFIG="$HOME/.kube/config" PATH="${PWD}/bin:$PATH" bash -x ./scripts/tests.e2e.sh --runtime=kube --image-name=localhost:5001/avalanchego-xsvm
