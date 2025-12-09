#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/lint_fix.sh ]]; then
  echo "must be run from coreth root"
  exit 255
fi

source ../evm-shared/scripts/lint_setup.sh
setup_lint
go tool -modfile=../../tools/go.mod golangci-lint run --config .golangci.yml --fix
go tool -modfile=../../tools/go.mod golangci-lint run --config "$AVALANCHE_LINT_FILE" --fix

