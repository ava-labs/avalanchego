#!/usr/bin/env bash

set -euo pipefail

# Tests that all devcontainer configurations build and start successfully
# using build_devcontainer.sh.

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

DEVCONTAINER_DIR="${AVALANCHE_PATH}/.devcontainer"

# Discover available configs
configs=()
for config in "${DEVCONTAINER_DIR}"/*/devcontainer.json; do
  [[ -f "${config}" ]] || continue
  configs+=("$(basename "$(dirname "${config}")")")
done

if [[ ${#configs[@]} -eq 0 ]]; then
  echo "Error: no devcontainer configs found in ${DEVCONTAINER_DIR}" >&2
  exit 1
fi

cleanup() {
  echo "Cleaning up devcontainer containers..."
  # devcontainer up labels containers with devcontainer.local_folder
  docker rm -f "$(docker ps -aq --filter "label=devcontainer.local_folder=${AVALANCHE_PATH}")" 2>/dev/null || true
}
trap cleanup EXIT

for name in "${configs[@]}"; do
  echo "=== Testing devcontainer config: ${name} ==="

  # Test the build via build_devcontainer.sh (which handles initializeCommand
  # internally, so this validates it works from a clean checkout).
  echo "Building devcontainer '${name}' via build_devcontainer.sh..."
  "${AVALANCHE_PATH}/scripts/build_devcontainer.sh" --build "${name}"

  # Start a container from the built image for smoke testing (using docker
  # directly to avoid devcontainer up rebuilding the image).
  echo "Starting container for smoke testing '${name}'..."
  IMAGE="$(docker images --format '{{.Repository}}' --filter "reference=vsc-$(basename "${AVALANCHE_PATH}")-*" 2>/dev/null | head -1)"
  if [[ -z "${IMAGE}" ]]; then
    echo "Error: no devcontainer image found after build." >&2
    exit 1
  fi
  CONTAINER_ID="$(docker run -d \
    -l "devcontainer.local_folder=${AVALANCHE_PATH}" \
    "${IMAGE}" \
    sleep infinity)"

  echo "Smoke-testing devcontainer '${name}'..."
  docker exec "${CONTAINER_ID}" nix develop --command sh -c \
    'go version && task --version && git rev-parse HEAD'

  echo "=== devcontainer '${name}' OK ==="
done

echo "All devcontainer configs passed."
