#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"

# Launcher policy:
# 1. Use a real `task` from PATH when available.
# 2. Otherwise, bootstrap task via `go tool` from tools/external.
# This launcher intentionally does not dispatch to `task` from PATH unless it
# excludes the repo-local wrapper, so aliases like `bin/task` can safely point
# here without recursion.
if task_bin="$(which -a task | grep -Fvx "${AVALANCHE_PATH}/bin/task" | head -n1)"; then
  exec "${task_bin}" "${@}"
fi

exec "${AVALANCHE_PATH}"/scripts/run_tool.sh task "${@}"
