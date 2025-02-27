#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests against nodes deployed to a kind cluster.

# TODO(marun)
# - Support testing against a remote cluster
# - Convert to golang to simplify reuse
# - Make idempotent to simplify development and debugging

if ! [[ "$0" =~ scripts/tests.e2e.kube.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

./scripts/ensure_kube_cluster.sh

KUBE_CONTEXT="${KUBE_CONTEXT:-kind-kind}"
# TODO(marun) Make the namespace configurable
NAMESPACE=tmpnet
PATH="${PWD}/bin:$PATH" kubectl create namespace "${NAMESPACE}" || true

if [[ -z "${SKIP_BUILD_IMAGE:-}" ]]; then
  bash -x ./scripts/build_xsvm_image.sh
fi

MONITORING_NAMESPACE=ci-monitoring
PATH="${PWD}/bin:$PATH" kubectl create namespace "${MONITORING_NAMESPACE}" || true

# Deploy promtail if credentials are available
if [[ -n "${LOKI_USERNAME:-}" && -n "${LOKI_PASSWORD:-}" ]]; then
  kubectl --namespace="${MONITORING_NAMESPACE}" create secret generic loki-credentials \
    --from-literal=username="${LOKI_USERNAME}" \
    --from-literal=password="${LOKI_PASSWORD}" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl --namespace="${MONITORING_NAMESPACE}" apply -f ./tests/fixture/tmpnet/yaml/promtail-daemonset.yaml
else
  echo "LOKI_USERNAME and LOKI_PASSWORD not set, skipping deployment of promtail"
fi

# Deploy prometheus agent if credentials are available
if [[ -n "${PROMETHEUS_USERNAME:-}" && -n "${PROMETHEUS_PASSWORD:-}" ]]; then
  kubectl --namespace="${MONITORING_NAMESPACE}" create secret generic prometheus-credentials \
    --from-literal=username="${PROMETHEUS_USERNAME}" \
    --from-literal=password="${PROMETHEUS_PASSWORD}" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl --namespace="${MONITORING_NAMESPACE}" apply -f ./tests/fixture/tmpnet/yaml/prometheus-agent.yaml
else
  echo "PROMETHEUS_USERNAME and PROMETHEUS_PASSWORD not set, skipping deployment of prometheus agent"
fi

E2E_SERIAL=1 KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}" PATH="${PWD}/bin:$PATH" \
  bash -x ./scripts/tests.e2e.sh --runtime=kube --image-name=localhost:5001/avalanchego-xsvm
