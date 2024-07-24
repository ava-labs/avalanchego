#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_bootstrap_tester.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh

echo "Building bootstrap tester..."
go build -o ./build/bootstrap-tester ./tests/bootstrap/
