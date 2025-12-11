#!/usr/bin/env bash

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
