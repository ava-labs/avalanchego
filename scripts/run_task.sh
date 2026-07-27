#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
CALLER_PATH="$(pwd)"

# e.g.,
# ./scripts/run_task.sh --list
# ./scripts/run_task.sh bazel-check-metadata
# RUN_TASK_PREFER_BAZEL=1 ./scripts/run_task.sh bazel-test-main   # CI path that gets task from Bazel
#
# Launcher policy:
# 1. Use a real `task` from PATH when available.
# 2. Otherwise, if RUN_TASK_PREFER_BAZEL=1, bootstrap task via Bazel.
# 3. Otherwise, bootstrap task via `go tool` from tools/external.
# This launcher intentionally does not dispatch to `task` from PATH unless it
# excludes the repo-local wrapper, so aliases like `bin/task` can safely point
# here without recursion.
if task_bin="$(which -a task 2>/dev/null | grep -Fvx "${AVALANCHE_PATH}/bin/task" | head -n1)"; then
  exec "${task_bin}" "${@}"
fi

if [[ "${RUN_TASK_PREFER_BAZEL-}" == "1" ]] && command -v bazelisk >/dev/null 2>&1; then
  cd "${AVALANCHE_PATH}"
  bazelisk build //tools/external:task >/dev/null
  task_bin="$(bazelisk cquery --output=files //tools/external:task 2>/dev/null)"
  if [[ "${task_bin}" != /* ]]; then
    task_bin="${AVALANCHE_PATH}/${task_bin}"
  fi
  cd "${CALLER_PATH}"
  exec "${task_bin}" "${@}"
fi

if command -v go >/dev/null 2>&1; then
  exec "${AVALANCHE_PATH}"/scripts/run_tool.sh task "${@}"
fi

cat >&2 <<'EOF'
Unable to launch task.
Expected one of:
  - task on PATH
  - go on PATH (for go tool -modfile=tools/external/go.mod task)
  - RUN_TASK_PREFER_BAZEL=1 with bazelisk on PATH (for bazelisk build/cquery //tools/external:task)
EOF
exit 127
