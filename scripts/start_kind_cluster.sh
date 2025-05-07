#!/usr/bin/env bash

set -euo pipefail

# Enable reuse of the arguments to ginkgo relevant to starting a cluster
START_CLUSTER_ARGS=()
for arg in "$@"; do
  if [[ "${arg}" =~ "--kubeconfig" || "${arg}" =~ "--kubeconfig-context" ]]; then
    START_CLUSTER_ARGS+=("${arg}")
  fi
done
./bin/tmpnetctl start-kind-cluster "${START_CLUSTER_ARGS[@]}"
