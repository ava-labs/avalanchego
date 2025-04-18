#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests against nodes deployed to a kind cluster.

# TODO(marun)
# - Support testing against a remote cluster

if ! [[ "$0" =~ scripts/tests.e2e.kube.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"

./bin/tmpnetctl start-kind-cluster

# Use an image that will be pushed to the local registry that the kind
# cluster is configured to use.
AVALANCHEGO_IMAGE="localhost:5001/avalanchego"
XSVM_IMAGE="${AVALANCHEGO_IMAGE}-xsvm"
if [[ -z "${SKIP_BUILD_IMAGE:-}" ]]; then
  XSVM_IMAGE="${XSVM_IMAGE}" AVALANCHEGO_IMAGE="${AVALANCHEGO_IMAGE}" bash -x ./scripts/build_xsvm_image.sh
fi

# Avoid having the test suite start local collectors since collection
# is only required from the nodes running in the kind cluster
TMPNET_START_COLLECTORS='' PATH="${PWD}/bin:$PATH" \
  bash -x ./scripts/tests.e2e.sh --runtime=kube --kube-image="${XSVM_IMAGE}"
