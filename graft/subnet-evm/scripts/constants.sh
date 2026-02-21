#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Avalanchego repository root
AVALANCHE_PATH=$(cd "$SUBNET_EVM_PATH" && cd ../.. && pwd)

# Source common constants from root
# shellcheck disable=SC1091
source "$AVALANCHE_PATH"/scripts/constants.sh
# shellcheck disable=SC1091
source "$AVALANCHE_PATH"/scripts/vcs.sh
image_tag="$(vcs_branch_or_tag)"

# Subnet-EVM specific constants
GOPATH="$(go env GOPATH)"
DEFAULT_PLUGIN_DIR="${HOME}/.avalanchego/plugins"
DEFAULT_VM_NAME="subnet-evm"
DEFAULT_VM_ID="srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"

# Docker image names
# Defaults to local to avoid unintentional pushes.
# For publishing, set: export IMAGE_NAME='avaplatform/subnet-evm'
IMAGE_NAME="${IMAGE_NAME:-subnet-evm}"
AVALANCHEGO_IMAGE_NAME="${AVALANCHEGO_IMAGE_NAME:-avaplatform/avalanchego}"

# Docker image tag defaults to branch/tag name
BUILD_IMAGE_ID="${BUILD_IMAGE_ID:-${image_tag}}"
