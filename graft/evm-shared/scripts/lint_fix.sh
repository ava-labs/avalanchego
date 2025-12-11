#!/usr/bin/env bash
#
# lint_fix.sh - Runs golangci-lint with automatic fixes
#
# Usage:
#   This script must be run from the root of a module that uses evm-shared
#   (e.g., coreth/ or subnet-evm/), NOT from evm-shared itself.
#
#   From the repository root:
#     ./scripts/lint_fix.sh
#
# Flow:
#   1. Runs golangci-lint with the module's own .golangci.yml config
#   2. Runs golangci-lint with avalanchego's .golangci.yml config (from ../../.golangci.yml)
#      - This second run excludes upstream files listed in scripts/upstream_files.txt
#
# Requirements:
#   - The module must have a .golangci.yml file at its root
#   - The module must have a scripts/upstream_files.txt file
#
# References (from avalanchego/ root):
#   - avalanchego/graft/{module}/.golangci.yml: Module's own lint config
#   - avalanchego/tools/go.mod: Avalanchego tools module
#   - avalanchego/.golangci.yml: Avalanchego lint config

set -euo pipefail

if ! [[ "$0" =~ scripts/lint_fix.sh ]]; then
  echo "must be run from module root"
  exit 255
fi

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck disable=SC1091
source "$SCRIPT_DIR/lint_setup.sh"
setup_lint
go tool -modfile=../../tools/go.mod golangci-lint run --config .golangci.yml --fix
go tool -modfile=../../tools/go.mod golangci-lint run --config "$AVALANCHE_LINT_FILE" --fix
