#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
# e.g.,
# ./scripts/run_task.sh --list
# ./scripts/run_task.sh bazel-check-metadata
#
# Launcher policy:
# 1. Use a real `task` from PATH when available.
# 2. Otherwise, bootstrap task via `go tool` from tools/external.
# This launcher intentionally does not dispatch to `task` from PATH unless it
# excludes the repo-local wrapper, so aliases like `bin/task` can safely point
# here without recursion.
if task_bin="$(which -a task 2>/dev/null | grep -Fvx "${AVALANCHE_PATH}/bin/task" | head -n1)"; then
  exec "${task_bin}" "${@}"
fi

if command -v go >/dev/null 2>&1; then
  # run_tool.sh sets GOWORK=off when it runs `go tool`. Without `-n`, `go tool`
  # passes this setting to Task. Task then passes it to each task command.
  # This setting disables the Go workspace. Workspace-wide tests then use the
  # root go.sum. That file does not contain transitive checksums for graft
  # modules.
  #
  # `go tool -n` builds Task if necessary. It prints the binary path without
  # running Task. Thus, GOWORK=off affects only the build. Task inherits the
  # caller's environment.
  task_bin="$("${AVALANCHE_PATH}"/scripts/run_tool.sh -n task)"
  if [[ -z "${task_bin}" ]]; then
    echo "Unable to resolve the task binary from tools/external." >&2
    exit 127
  fi
  exec "${task_bin}" "${@}"
fi

cat >&2 <<'EOF'
Unable to launch task.
Expected one of:
  - task on PATH
  - go on PATH (for go tool -modfile=tools/external/go.mod task)
EOF
exit 127
