#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"

# Launcher policy:
# 1. Use a real `task` from PATH when available.
# 2. Otherwise, run the Bazel-owned fallback target for task.
# This launcher intentionally does not dispatch to `task` from PATH unless it
# excludes the repo-local wrapper, so aliases like `bin/task` can safely point
# here without recursion.
if task_bin="$(which -a task 2>/dev/null | grep -Fvx "${AVALANCHE_PATH}/bin/task" | head -n1)"; then
  exec "${task_bin}" "${@}"
fi

if command -v bazel >/dev/null 2>&1; then
  cd "${AVALANCHE_PATH}"
  bazel build //tools/external:task >/dev/null
  task_bin="$(bazel cquery --output=files //tools/external:task 2>/dev/null)"
  exec "${task_bin}" "${@}"
fi

cat >&2 <<'EOF'
Unable to launch task.
Expected one of:
  - task on PATH
  - bazel on PATH (for bazel build/cquery //tools/external:task)
EOF
exit 127
