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

# Run the initializeCommand from a devcontainer.json config. This is needed
# because 'devcontainer build' (used by build_devcontainer.sh --build) does
# not execute initializeCommand, but the Dockerfile may depend on files it
# creates.
run_initialize_command() {
  local config_path="$1"
  if ! jq -e '.initializeCommand' "${config_path}" > /dev/null 2>&1; then
    return
  fi
  echo "Running initializeCommand from ${config_path}..."
  local -a init_cmd=()
  while IFS= read -r elem; do
    init_cmd+=("$elem")
  done < <(jq -r '.initializeCommand[]' "${config_path}")
  (cd "${AVALANCHE_PATH}" && "${init_cmd[@]}")
}

cleanup() {
  echo "Cleaning up devcontainer containers..."
  # devcontainer up labels containers with devcontainer.local_folder
  docker rm -f "$(docker ps -aq --filter "label=devcontainer.local_folder=${AVALANCHE_PATH}")" 2>/dev/null || true
}
trap cleanup EXIT

for name in "${configs[@]}"; do
  config_path="${DEVCONTAINER_DIR}/${name}/devcontainer.json"
  echo "=== Testing devcontainer config: ${name} ==="

  # Run initializeCommand so that build_devcontainer.sh --build works on a
  # clean checkout.
  run_initialize_command "${config_path}"

  # Test the build via build_devcontainer.sh.
  echo "Building devcontainer '${name}' via build_devcontainer.sh..."
  "${AVALANCHE_PATH}/scripts/build_devcontainer.sh" --build "${name}"

  # Start the container for smoke testing.
  echo "Starting devcontainer '${name}'..."
  devcontainer up \
    --workspace-folder "${AVALANCHE_PATH}" \
    --config "${config_path}"

  echo "Smoke-testing devcontainer '${name}'..."
  devcontainer exec \
    --workspace-folder "${AVALANCHE_PATH}" \
    --config "${config_path}" \
    nix develop --command go version

  devcontainer exec \
    --workspace-folder "${AVALANCHE_PATH}" \
    --config "${config_path}" \
    nix develop --command task --version

  devcontainer exec \
    --workspace-folder "${AVALANCHE_PATH}" \
    --config "${config_path}" \
    nix develop --command git rev-parse HEAD

  echo "=== devcontainer '${name}' OK ==="
done

echo "All devcontainer configs passed."
