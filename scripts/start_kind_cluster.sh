#!/usr/bin/env bash

set -euo pipefail

# --kubeconfig and --kubeconfig-context should be provided in the form --arg=value
# to work with the simplistic mechanism enabling flag reuse.

# Enable reuse of the arguments to ginkgo relevant to starting a cluster
START_CLUSTER_ARGS=()
for arg in "$@"; do
  if [[ "${arg}" =~ "--kubeconfig=" || "${arg}" =~ "--kubeconfig-context=" || "${arg}" =~ "--start-metrics-collector" || "${arg}" =~ "--start-logs-collector" ]]; then
    START_CLUSTER_ARGS+=("${arg}")
  fi
done
echo "Starting kind cluster with args: ${START_CLUSTER_ARGS[*]}"
echo "To cleanup the cluster, run ./scripts/run_task.sh delete-kind-cluster"
./bin/tmpnetctl start-kind-cluster "${START_CLUSTER_ARGS[@]}"
