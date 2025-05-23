#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests against nodes deployed to a kind cluster.

# TODO(marun) Support testing against a remote cluster

if ! [[ "$0" =~ scripts/tests.e2e.kube.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# This script will use kubeconfig arguments if supplied
./scripts/start_kind_cluster.sh "$@"

# Use an image that will be pushed to the local registry that the kind cluster is configured to use.
AVALANCHEGO_IMAGE="localhost:5001/avalanchego"
XSVM_IMAGE="${AVALANCHEGO_IMAGE}-xsvm"
if [[ -n "${SKIP_BUILD_IMAGE:-}" ]]; then
  echo "Skipping build of xsvm image due to SKIP_BUILD_IMAGE=${SKIP_BUILD_IMAGE}"
else
  XSVM_IMAGE="${XSVM_IMAGE}" AVALANCHEGO_IMAGE="${AVALANCHEGO_IMAGE}" bash -x ./scripts/build_xsvm_image.sh
fi

bash -x ./scripts/tests.e2e.sh --runtime=kube --kube-image="${XSVM_IMAGE}" "$@"
