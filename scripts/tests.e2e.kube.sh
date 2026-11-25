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
source ./scripts/image_tag.sh
AVALANCHEGO_IMAGE="localhost:5001/avalanchego"
AVALANCHEGO_IMAGE_REF="${KUBE_AVALANCHEGO_IMAGE:-${AVALANCHEGO_IMAGE}:${image_tag}}"
if [[ -n "${KUBE_AVALANCHEGO_IMAGE:-}" && -z "${E2E_SKIP_XSVM:-}" ]]; then
  echo 'KUBE_AVALANCHEGO_IMAGE requires E2E_SKIP_XSVM' >&2
  exit 1
fi
XSVM_IMAGE="${AVALANCHEGO_IMAGE}-xsvm"
if [[ -n "${E2E_SKIP_XSVM:-}" ]]; then
  if [[ -z "${KUBE_AVALANCHEGO_IMAGE:-}" ]]; then
    echo 'E2E_SKIP_XSVM requires KUBE_AVALANCHEGO_IMAGE' >&2
    exit 1
  fi
  XSVM_IMAGE="${AVALANCHEGO_IMAGE_REF}"
  echo "Skipping XSVM wrapper; using ${XSVM_IMAGE} directly"
elif [[ -n "${SKIP_BUILD_IMAGE:-}" ]]; then
  echo "Skipping build of xsvm image due to SKIP_BUILD_IMAGE=${SKIP_BUILD_IMAGE}"
else
  XSVM_IMAGE="${XSVM_IMAGE}" AVALANCHEGO_IMAGE="${AVALANCHEGO_IMAGE}" bash -x ./scripts/build_xsvm_image.sh
fi

# Determine kubeconfig context to use
KUBECONFIG_CONTEXT=""

# Check if --kubeconfig-context is already provided in arguments
if [[ "$*" =~ --kubeconfig-context ]]; then
    # User provided a context, use it as-is
    echo "Using provided kubeconfig context from arguments"
else
    # Default to the RBAC context
    KUBECONFIG_CONTEXT="--kubeconfig-context=kind-kind-tmpnet"
    echo "Defaulting to limited-permission context 'kind-kind-tmpnet' to test RBAC Role permissions"
fi

echo "Running kube e2e with image ${XSVM_IMAGE} (base ${AVALANCHEGO_IMAGE_REF})"
bash -x ./scripts/tests.e2e.sh --runtime=kube --kube-image="${XSVM_IMAGE}" "$KUBECONFIG_CONTEXT" "$@"
