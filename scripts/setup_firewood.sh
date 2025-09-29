#!/usr/bin/env bash

# Setup Firewood FFI
#
# Clones Firewood repository, builds/fetches the FFI, and updates go.mod
#
# Usage:
#   setup_firewood.sh <version>
#
# Arguments:
#   version       Firewood version (ffi/vX.Y.Z for pre-built, commit/branch for source)
#
# Output:
#   Prints FFI path to stdout on success

set -euo pipefail

if [ $# -lt 1 ]; then
    echo "Usage: $0 <version>" >&2
    exit 1
fi

FIREWOOD_VERSION="$1"
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
FIREWOOD_CLONE_DIR="${AVALANCHE_PATH}/firewood"

if [ -d "${FIREWOOD_CLONE_DIR}" ]; then
  echo "Removing existing Firewood directory..." >&2
  rm -rf "${FIREWOOD_CLONE_DIR}"
fi

echo "Setting up Firewood FFI version: ${FIREWOOD_VERSION}" >&2

git clone https://github.com/ava-labs/firewood "${FIREWOOD_CLONE_DIR}" \
  --quiet --depth 1 --branch composable-ci-action

SETUP_FIREWOOD_SCRIPT="${FIREWOOD_CLONE_DIR}/.github/scripts/build.sh"

if [ ! -f "${SETUP_FIREWOOD_SCRIPT}" ]; then
  echo "Error: Setup Firewood script not found at ${SETUP_FIREWOOD_SCRIPT}" >&2
  exit 1
fi

# Build or fetch Firewood FFI
# Capture only the last line which is the FFI path
FFI_PATH=$("${SETUP_FIREWOOD_SCRIPT}" "${FIREWOOD_VERSION}" 2>&1 | tail -n 1)

if [ -z "${FFI_PATH}" ]; then
  echo "Error: Failed to build/fetch Firewood FFI" >&2
  exit 1
fi

cd "${AVALANCHE_PATH}"

# Verify go.mod exists
if [ ! -f "go.mod" ]; then
  echo "Error: go.mod not found in ${AVALANCHE_PATH}" >&2
  exit 1
fi

echo "Updating go.mod with FFI path: ${FFI_PATH}" >&2
go mod edit -replace github.com/ava-labs/firewood-go-ethhash/ffi="${FFI_PATH}"

go mod tidy
go mod download

# Output FFI path to stdout for consumption by other scripts
echo "${FFI_PATH}"
