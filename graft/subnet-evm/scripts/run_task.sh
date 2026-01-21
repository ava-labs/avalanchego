#!/usr/bin/env bash

set -euo pipefail

# Assume the system-installed task is compatible with the taskfile version
if command -v task > /dev/null 2>&1; then
  exec task "${@}"
else
  go tool -modfile=../../tools/external/go.mod task "${@}"
fi
