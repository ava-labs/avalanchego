#!/usr/bin/env bash

set -euo pipefail

# Checks that the Go Task bootstrap version matches the Task version supplied by
# the Nix development shell. Nix is the authoritative Task version source.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

NIX_TASK_VERSION="$(nix develop --command task --version)"
if ! [[ "$NIX_TASK_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: failed to parse Task version from Nix: $NIX_TASK_VERSION" >&2
  exit 1
fi

GO_TASK_VERSION="$(awk '$1 == "github.com/go-task/task/v3" { print $2 }' tools/external/go.mod)"
if ! [[ "$GO_TASK_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: failed to parse Task version from tools/external/go.mod" >&2
  exit 1
fi

if [[ "$GO_TASK_VERSION" != "v$NIX_TASK_VERSION" ]]; then
  echo "Task version mismatch: Nix provides $NIX_TASK_VERSION, tools/external/go.mod requires $GO_TASK_VERSION" >&2
  echo "Run 'task sync-task-version' to synchronize tools/external/go.mod." >&2
  exit 1
fi

for ARCHIVE in \
  task_darwin_arm64.tar.gz \
  task_linux_amd64.tar.gz \
  task_linux_arm64.tar.gz; do
  TASK_VERSION="$GO_TASK_VERSION" ./scripts/setup_task.sh checksum "$ARCHIVE" >/dev/null
done

echo "Task versions and archive checksums are consistent: $NIX_TASK_VERSION"
