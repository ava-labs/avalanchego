#!/usr/bin/env bash

set -euo pipefail

# Checks that the Go Task bootstrap version matches the Task version supplied by
# the Nix development shell. Nix is the authoritative Task version source.

if ! [[ "$0" =~ scripts/check_task_version.sh ]]; then
  echo "must be run from repository root" >&2
  exit 255
fi

nix_task_version="$(task --version)"
if ! [[ "$nix_task_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: failed to parse Task version from Nix: $nix_task_version" >&2
  exit 1
fi

go_task_version="$(awk '$1 == "github.com/go-task/task/v3" { print $2 }' tools/external/go.mod)"
if ! [[ "$go_task_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: failed to parse Task version from tools/external/go.mod" >&2
  exit 1
fi

if [[ "$go_task_version" != "v$nix_task_version" ]]; then
  echo "Task version mismatch: Nix provides $nix_task_version, tools/external/go.mod requires $go_task_version" >&2
  echo "Run 'task sync-task-version' to synchronize tools/external/go.mod." >&2
  exit 1
fi

echo "Task versions are consistent: $nix_task_version"
