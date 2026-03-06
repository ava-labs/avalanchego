#!/usr/bin/env bash

set -euo pipefail

# Assume the system-installed task is compatible with the taskfile version
if command -v task > /dev/null 2>&1; then
  exec task "${@}"
else
  REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && cd ../../.. && pwd)
  "$REPO_ROOT"/scripts/run_tool.sh task "${@}"
fi
