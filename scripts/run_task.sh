#!/usr/bin/env bash

set -euo pipefail

# Assume the system-installed task is compatible with the taskfile version
if command -v task > /dev/null 2>&1; then
  exec task "${@}"
else
  AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
  "${AVALANCHE_PATH}"/scripts/run_tool.sh task "${@}"
fi
