#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/lint_fix.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/lint_setup.sh
setup_lint
./scripts/run_tool.sh golangci-lint run --config .legacy-golangci.yml --fix ./graft/...
./scripts/run_tool.sh golangci-lint run --config "$AVALANCHE_LINT_FILE" --fix
