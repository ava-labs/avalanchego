#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests for bootstrap monitor.

if ! [[ "$0" =~ scripts/tests.e2e.kube.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

./scripts/start_kind_cluster.sh

# TODO(marun) Make the namespace configurable
PATH="${PWD}/bin:$PATH" kubectl create namespace tmpnet

# TODO(marun) Is the path still necessary?
KUBECONFIG="$HOME/.kube/config" PATH="${PWD}/bin:$PATH" ./scripts/tests.e2e.sh --runtime=kube
