#!/usr/bin/env bash
#
# lint_fix.sh - Runs golangci-lint with automatic fixes
#
# Usage:
#   This script must be run from the root of a module that uses evm
#   (e.g., coreth/ or subnet-evm/), NOT from evm itself.
#
#   From the repository root:
#     ./scripts/lint_fix.sh
#
# Flow:
#   1. Runs golangci-lint with the shared graft .golangci.yml config
#   2. Runs golangci-lint with avalanchego's .golangci.yml config (from ../../.golangci.yml)
#      - This second run excludes upstream files listed in scripts/upstream_files.txt
#
# Requirements:
#   - The module must have a scripts/upstream_files.txt file
#
# References (from avalanchego/ root):
#   - avalanchego/graft/.golangci.yml: Shared lint config for graft modules
#   - avalanchego/tools/external/go.mod: Avalanchego tools module
#   - avalanchego/.golangci.yml: Avalanchego lint config

set -euo pipefail

if ! [[ "$0" =~ scripts/lint_fix.sh ]]; then
  echo "must be run from module root"
  exit 255
fi

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
RUN_TOOL="$SCRIPT_DIR/../../../scripts/run_tool.sh"

# shellcheck disable=SC1091
source "$SCRIPT_DIR/lint_setup.sh"
setup_lint
"$RUN_TOOL" golangci-lint run --config ../.golangci.yml --fix
"$RUN_TOOL" golangci-lint run --config "$AVALANCHE_LINT_FILE" --fix
