#!/usr/bin/env bash

set -euo pipefail

DIAGNOSTICS_DIR="${1:-${KIND_DIAGNOSTICS_DIR:-$(mktemp -d)}}"
mkdir -p "${DIAGNOSTICS_DIR}"

run_capture() {
  local output_file="$1"
  shift

  {
    echo "$ $*"
    "$@"
  } > "${output_file}" 2>&1 || true
}

{
  date -u
  uname -a
  free -h
  df -h
  docker system df
  docker ps -a
  docker images
} > "${DIAGNOSTICS_DIR}/runner.txt" 2>&1 || true

run_capture "${DIAGNOSTICS_DIR}/docker-inspect.json" docker inspect kind-control-plane kind-registry
run_capture "${DIAGNOSTICS_DIR}/kind-control-plane.log" docker logs kind-control-plane
run_capture "${DIAGNOSTICS_DIR}/kind-registry.log" docker logs kind-registry

if kind get clusters 2>/dev/null | grep -q "^kind$"; then
  run_capture "${DIAGNOSTICS_DIR}/kind-export.log" kind export logs "${DIAGNOSTICS_DIR}/kind-logs"
else
  echo "kind cluster not found" > "${DIAGNOSTICS_DIR}/kind-export.log"
fi

{
  kind get clusters
  kubectl cluster-info
  kubectl get nodes -o wide
  kubectl get pods -A -o wide
  kubectl get events -A --sort-by=.lastTimestamp
  kubectl describe nodes
  kubectl describe pods -A
} > "${DIAGNOSTICS_DIR}/kubernetes.txt" 2>&1 || true

find "${DIAGNOSTICS_DIR}" -type f -print
