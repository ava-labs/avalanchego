#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"

# Launcher policy:
# 1. Use the current PATH when the repo flake environment is already active.
# 2. Otherwise, use the nix-pinned task when nix is available.
# 3. Otherwise, bootstrap task via `go tool` from tools/external.
# This launcher intentionally does not dispatch to `task` from PATH so repo-local
# aliases like `bin/task` can safely point here without recursion.
if [[ -n "${AVALANCHEGO_FLAKE_ACTIVE-}" ]]; then
  task_bin="$(which -a task | grep -Fvx "${AVALANCHE_PATH}/bin/task" | head -n1)"
  exec "${task_bin}" "${@}"
fi

if command -v nix > /dev/null 2>&1; then
  # shellcheck disable=SC2016
  exec nix develop "${AVALANCHE_PATH}" --command bash -c '
    set -euo pipefail
    task_bin="$(which -a task | grep -Fvx "$1/bin/task" | head -n1)"
    exec "${task_bin}" "${@:2}"
  ' bash "${AVALANCHE_PATH}" "${@}"
fi

exec "${AVALANCHE_PATH}"/scripts/run_tool.sh task "${@}"
