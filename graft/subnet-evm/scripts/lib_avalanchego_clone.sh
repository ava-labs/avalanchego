#!/usr/bin/env bash

set -euo pipefail

# Defines functions for interacting with git clones of the avalanchego repo.

if [[ -z "${SUBNET_EVM_PATH}" ]]; then
  echo "SUBNET_EVM_PATH must be set"
  exit 1
fi

export AVALANCHEGO_CLONE_PATH=${AVALANCHEGO_CLONE_PATH:-${SUBNET_EVM_PATH}/avalanchego}

# Clones the avalanchego repo to the configured path and checks out the specified version.
function clone_avalanchego {
  local avalanche_version="$1"

  echo "checking out target avalanchego version ${avalanche_version} to ${AVALANCHEGO_CLONE_PATH}"
  if [[ -d "${AVALANCHEGO_CLONE_PATH}" ]]; then
    echo "updating existing clone"
    cd "${AVALANCHEGO_CLONE_PATH}"
    git fetch
  else
    echo "creating new clone"
    git clone https://github.com/ava-labs/avalanchego.git "${AVALANCHEGO_CLONE_PATH}"
    cd "${AVALANCHEGO_CLONE_PATH}"
  fi
  # Branch will be reset to $avalanche_version if it already exists
  git checkout -B "test-${avalanche_version}" "${avalanche_version}"
  cd "${SUBNET_EVM_PATH}"
}

# Derives an image tag from the current state of the avalanchego clone
function avalanchego_image_tag_from_clone {
  local commit_hash
  commit_hash="$(git --git-dir="${AVALANCHEGO_CLONE_PATH}/.git" rev-parse HEAD)"
  echo "${commit_hash::8}"
}
