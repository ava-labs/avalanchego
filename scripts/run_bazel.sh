#!/usr/bin/env bash

set -euo pipefail

# Use system bazel if available, otherwise run via nix flake
if command -v bazel > /dev/null 2>&1; then
  exec bazel "${@}"
else
  AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
  exec nix develop "${AVALANCHE_PATH}" --command bazel "${@}"
fi
