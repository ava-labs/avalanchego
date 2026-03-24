#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"

# Launcher policy:
# 1. Inside the nix dev shell, prefer the in-shell task.
# 2. Outside the nix dev shell, use the nix-pinned task when nix is available.
# 3. Otherwise, bootstrap task via `go tool` from tools/external.
if [[ -n "${IN_NIX_SHELL-}" ]] && command -v task > /dev/null 2>&1; then
  exec task "${@}"
fi

if command -v nix > /dev/null 2>&1; then
  exec "${AVALANCHE_PATH}"/scripts/nix_run.sh task "${@}"
fi

if command -v task > /dev/null 2>&1; then
  exec task "${@}"
fi

exec "${AVALANCHE_PATH}"/scripts/run_tool.sh task "${@}"
