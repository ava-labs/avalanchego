#!/usr/bin/env bash

set -euo pipefail

# Run e2e tests for bootstrap monitor.

if ! [[ "$0" =~ scripts/tests.e2e.bootstrap_monitor.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Determine DIST and ARCH in case installation is required for kubectl and kind
#
# TODO(marun) Factor this out for reuse
if which sw_vers &> /dev/null; then
  OS="darwin"
  ARCH="$(uname -m)"
else
  # Assume linux (windows is not supported)
  OS="linux"
  RAW_ARCH="$(uname -i)"
  # Convert the linux arch string to the string used for k8s releases
  if [[ "${RAW_ARCH}" == "aarch64" ]]; then
    ARCH="arm64"
  elif [[ "${RAW_ARCH}" == "x86_64" ]]; then
    ARCH="amd64"
  else
    echo "Unsupported architecture: ${RAW_ARCH}"
    exit 1
  fi
fi

function ensure_command {
  local cmd=$1
  local install_uri=$2

  if ! command -v "${cmd}" &> /dev/null; then
    # Try to use a local version
    local local_cmd="${PWD}/bin/${cmd}"
    mkdir -p "${PWD}/bin"
    if ! command -v "${local_cmd}" &> /dev/null; then
      echo "${cmd} not found, attempting to install..."
      curl -L -o "${local_cmd}" "${install_uri}"
      # TODO(marun) Optionally validate the binary against published checksum
      chmod +x "${local_cmd}"
    fi
  fi
}

# Ensure the kubectl command is available
KUBECTL_VERSION=v1.30.2
ensure_command kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl"

# Ensure the kind command is available
KIND_VERSION=v0.23.0
ensure_command kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-${OS}-${ARCH}"

# Ensure the kind-with-registry command is available
ensure_command "kind-with-registry.sh" "https://raw.githubusercontent.com/kubernetes-sigs/kind/7cb9e6be25b48a0e248097eef29d496ab1a044d0/site/static/examples/kind-with-registry.sh"

# Deploy a kind cluster with a local registry. Include the local bin in the path to
# ensure locally installed kind and kubectl are available since the script expects to
# call them without a qualifying path.
PATH="${PWD}/bin:$PATH" bash -x "${PWD}/bin/kind-with-registry.sh"

# TODO(marun) Factor out ginkgo installation to avoid duplicating it across test scripts
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.13.1

KUBECONFIG="$HOME/.kube/config" PATH="${PWD}/bin:$PATH" ginkgo -v ./tests/fixture/bootstrapmonitor/e2e
