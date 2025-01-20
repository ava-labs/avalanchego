#!/usr/bin/env bash

set -euo pipefail

# This script ensures that a kubernetes cluster is available with the
# default kubeconfig. If a cluster is not already running, kind will
# be used to start one.

if ! [[ "$0" =~ scripts/ensure_kube_cluster.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

function ensure_command {
  local cmd=$1
  local install_uri=$2

  echo "Ensuring ${cmd} is available"
  if ! command -v "${cmd}" &> /dev/null; then
    local local_cmd="${PWD}/bin/${cmd}"
    mkdir -p "${PWD}/bin"
    echo "${cmd} not found, attempting to install..."
    curl -L -o "${local_cmd}" "${install_uri}"
    # TODO(marun) Optionally validate the binary against published checksum
    chmod +x "${local_cmd}"
  fi
}

# Enables using a context other than the current default
KUBE_CONTEXT="${KUBE_CONTEXT:-}"

# Ensure locally-installed binaries are in the path
PATH="${PWD}/bin:$PATH"

# Determine the platform to download binaries for
GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"

KUBECTL_VERSION=v1.30.2
ensure_command kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${GOOS}/${GOARCH}/kubectl"

# Compose the kubectl command
KUBECTL_CMD="kubectl"
if [[ -n "${KUBE_CONTEXT}" ]]; then
  KUBECTL_CMD="${KUBECTL_CMD} --context=${KUBE_CONTEXT}"
fi

# Check if a cluster is already running
if ${KUBECTL_CMD} cluster-info &> /dev/null; then
  echo "A kube cluster is already accessible"
  exit 0
fi

KIND_VERSION=v0.23.0
ensure_command kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-${GOOS}-${GOARCH}"

SCRIPT_SHA=7cb9e6be25b48a0e248097eef29d496ab1a044d0
ensure_command "kind-with-registry.sh" \
  "https://raw.githubusercontent.com/kubernetes-sigs/kind/${SCRIPT_SHA}/site/static/examples/kind-with-registry.sh"

echo "Deploying a new kind cluster with a local registry"
bash -x "${PWD}/bin/kind-with-registry.sh"
