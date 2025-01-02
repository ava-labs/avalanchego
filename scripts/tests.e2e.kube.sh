#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests for bootstrap monitor.

if ! [[ "$0" =~ scripts/tests.e2e.kube.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

./scripts/start_kind_cluster.sh

# TODO(marun) Make the namespace configurable
PATH="${PWD}/bin:$PATH" kubectl create namespace tmpnet || true

bash -x ./scripts/build_xsvm_image.sh

# TODO(marun) Is the path still necessary?
E2E_SERIAL=1 KUBECONFIG="$HOME/.kube/config" PATH="${PWD}/bin:$PATH" bash -x ./scripts/tests.e2e.sh --runtime=kube --image-name=localhost:5001/avalanchego-xsvm:latest
