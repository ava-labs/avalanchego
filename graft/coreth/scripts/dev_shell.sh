#!/usr/bin/env bash
# Copyright (C) 2025, Ava Labs, Inc. All rights reserved.

set -euo pipefail

# Requires nix to be installed. The determinate systems installer is recommended:
#
#   https://github.com/DeterminateSystems/nix-installer?tab=readme-ov-file#install-nix
#

# Load AVALANCHE_VERSION
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=/scripts/constants.sh
source "$SCRIPT_DIR"/versions.sh

# Start a dev shell with the avalanchego flake
FLAKE="github:ava-labs/avalanchego?ref=${AVALANCHE_VERSION}"
echo "Starting nix shell for ${FLAKE}"
nix develop "${FLAKE}" "${@}"
