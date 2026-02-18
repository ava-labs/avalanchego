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
source "$AVALANCHE_PATH"/scripts/git_commit.sh
# shellcheck disable=SC1091
source "$AVALANCHE_PATH"/scripts/image_tag.sh

# Subnet-EVM specific constants
GOPATH="$(go env GOPATH)"
DEFAULT_PLUGIN_DIR="${HOME}/.avalanchego/plugins"
DEFAULT_VM_ID="srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"
