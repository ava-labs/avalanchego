#!/usr/bin/env bash

set -euo pipefail

print_usage() {
  printf "Usage: build_firewood.sh [FIREWOOD_COMMIT] [--update]

  Build Firewood FFI from source for testing with Coreth.

  Arguments:
    FIREWOOD_COMMIT   Git commit or branch to build (default: main)

  Options:
    --update          Update go.mod after building
                      (default: only print path)

  Examples:
    # Build from commit and capture FFI path
    FIREWOOD_FFI_PATH=\$(./scripts/build_firewood.sh abc123def)

    # Build from main and update go.mod automatically
    ./scripts/build_firewood.sh main --update

  Note: To use a published Firewood version (e.g., v0.0.15) run go get -u github.com/ava-labs/firewood-go-ethhash/ffi@{version}
"
}

FIREWOOD_COMMIT="main"
UPDATE_GO_MOD=false

while [[ $# -gt 0 ]]; do
  case $1 in
    -h|--help)
      print_usage
      exit 0
      ;;
    --update)
      UPDATE_GO_MOD=true
      shift
      ;;
    *)
      FIREWOOD_COMMIT="$1"
      shift
      ;;
  esac
done

CORETH_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
FIREWOOD_DIR="${CORETH_PATH}/build/firewood"

echo "Building Firewood FFI from source at: ${FIREWOOD_COMMIT}" >&2

if ! command -v nix &> /dev/null; then
  echo "Error: nix command not found" >&2
  echo "Nix is required to build Firewood. To install it, run:" >&2
  echo "  ./scripts/run_task.sh install-nix" >&2
  exit 1
fi

if [ -d "${FIREWOOD_DIR}/.git" ]; then
  cd "${FIREWOOD_DIR}"
  git fetch origin
  git checkout "${FIREWOOD_COMMIT}"
  git pull --ff-only 2>/dev/null || true
else
  mkdir -p "${CORETH_PATH}/build"
  git clone https://github.com/ava-labs/firewood.git "${FIREWOOD_DIR}"
  cd "${FIREWOOD_DIR}"
  git checkout "${FIREWOOD_COMMIT}"
fi

cd "${FIREWOOD_DIR}/ffi"
nix build 2>&1

FIREWOOD_FFI_PATH="${FIREWOOD_DIR}/ffi/result/ffi"
export FIREWOOD_FFI_PATH

if [ ! -d "${FIREWOOD_FFI_PATH}" ]; then
  echo "Error: Build succeeded but result not found at ${FIREWOOD_FFI_PATH}" >&2
  exit 1
fi

echo "Firewood built successfully at: ${FIREWOOD_FFI_PATH}" >&2

if [ "${UPDATE_GO_MOD}" = true ]; then
  echo "Updating go.mod..." >&2
  
  cd "${CORETH_PATH}"
  go mod edit -replace "github.com/ava-labs/firewood-go-ethhash/ffi=${FIREWOOD_FFI_PATH}"
  go mod tidy
  
  echo "Updated go.mod successfully" >&2
fi

# Output path to stdout for capture
echo "${FIREWOOD_FFI_PATH}"

