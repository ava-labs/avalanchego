#!/usr/bin/env bash

set -euo pipefail

# Repo root
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

DEVCONTAINER_DIR="${AVALANCHE_PATH}/.devcontainer"

# Parse mode flag (default: build and run)
MODE="build-and-run"
if [[ ${1:-} == "--build" ]]; then
  MODE="build"
  shift
elif [[ ${1:-} == "--run" ]]; then
  MODE="run"
  shift
fi

# Discover available configs by scanning for devcontainer.json files
available_configs=()
for config in "${DEVCONTAINER_DIR}"/*/devcontainer.json; do
  [[ -f "${config}" ]] || continue
  name="$(basename "$(dirname "${config}")")"
  available_configs+=("${name}")
done

if [[ ${#available_configs[@]} -eq 0 ]]; then
  echo "Error: no devcontainer configs found in ${DEVCONTAINER_DIR}" >&2
  exit 1
fi

# No config arg: list available configs
if [[ $# -eq 0 ]]; then
  echo "Available devcontainer configs:"
  for name in "${available_configs[@]}"; do
    echo "  ${name}"
  done
  echo ""
  echo "Usage: $0 [--build|--run] <config-name>"
  exit 0
fi

CONFIG_NAME="$1"
CONFIG_PATH="${DEVCONTAINER_DIR}/${CONFIG_NAME}/devcontainer.json"

# Validate the requested config exists
if [[ ! -f "${CONFIG_PATH}" ]]; then
  echo "Error: unknown config '${CONFIG_NAME}'" >&2
  echo "" >&2
  echo "Available devcontainer configs:" >&2
  for name in "${available_configs[@]}"; do
    echo "  ${name}" >&2
  done
  exit 1
fi

# Run the initializeCommand from a devcontainer.json config. 'devcontainer
# build' does not execute initializeCommand, but the Dockerfile may depend on
# files it creates, so we run it explicitly before building.
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

if [[ "${MODE}" == "build" ]]; then
  run_initialize_command "${CONFIG_PATH}"
  echo "Building devcontainer '${CONFIG_NAME}'..."
  devcontainer build \
    --workspace-folder "${AVALANCHE_PATH}" \
    --config "${CONFIG_PATH}"
fi

if [[ "${MODE}" != "build" ]]; then
  # Check if a container for this config is already running.
  CONTAINER_ID="$(docker ps -q --filter "label=devcontainer.config_file=${CONFIG_PATH}" 2>/dev/null || true)"

  if [[ -z "${CONTAINER_ID}" ]]; then
    echo "Starting devcontainer '${CONFIG_NAME}'..."
    devcontainer up \
      --workspace-folder "${AVALANCHE_PATH}" \
      --config "${CONFIG_PATH}"
  else
    echo "Devcontainer '${CONFIG_NAME}' is already running (${CONTAINER_ID})."
  fi

  echo "Entering devcontainer '${CONFIG_NAME}'..."
  devcontainer exec \
    --workspace-folder "${AVALANCHE_PATH}" \
    --config "${CONFIG_PATH}" \
    nix develop --command bash
fi
