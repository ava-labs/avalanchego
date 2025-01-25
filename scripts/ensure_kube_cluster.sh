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
    mkdir -p "${PWD}/bin"
    echo "${cmd} not found, attempting to install..."
    if [[ "${cmd}" == helm ]]; then
      curl -L -o - "${install_uri}" | tar -xz -C "${PWD}/bin" --strip-components=1 "${GOOS}-${GOARCH}/${cmd}"
    else
      local local_cmd="${PWD}/bin/${cmd}"
      curl -L -o "${local_cmd}" "${install_uri}"
      chmod +x "${local_cmd}"
    fi
    # TODO(marun) Optionally validate the binary against published checksum
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
else
  KIND_VERSION=v0.23.0
  ensure_command kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-${GOOS}-${GOARCH}"

  KIND_SCRIPT_SHA=7cb9e6be25b48a0e248097eef29d496ab1a044d0
  ensure_command "kind-with-registry.sh" \
    "https://raw.githubusercontent.com/kubernetes-sigs/kind/${KIND_SCRIPT_SHA}/site/static/examples/kind-with-registry.sh"

  echo "Deploying a new kind cluster with a local registry"
  bash -x "$(command -v kind-with-registry.sh)"
fi

# WARNING This is only intended to work for the runtime configuration of a kind cluster.
if [[ -n "${INSTALL_CHAOS_MESH:-}" ]]; then
  # Ensure helm is available
  HELM_VERSION=v3.7.0
  ensure_command helm "https://get.helm.sh/helm-${HELM_VERSION}-${GOOS}-${GOARCH}.tar.gz"

  # Install chaos mesh via helm
  helm repo add chaos-mesh https://charts.chaos-mesh.org

  # Create the namespace to install to
  ${KUBECTL_CMD} create ns chaos-mesh

  # Install chaos mesh for containerd, with dashboard persistence and no security, and without leader election
  CHAOS_MESH_VERSION=2.7.0
  helm install chaos-mesh chaos-mesh/chaos-mesh -n=chaos-mesh --version "${CHAOS_MESH_VERSION}"\
    --set chaosDaemon.runtime=containerd\
    --set chaosDaemon.socketPath=/run/containerd/containerd.sock\
    --set dashboard.persistentVolume.enabled=true\
    --set dashboard.persistentVolume.storageClass=standard\
    --set dashboard.securityMode=false\
    --set controllerManager.leaderElection.enabled=false
fi
