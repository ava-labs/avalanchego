#!/usr/bin/env bash

set -euo pipefail

# Synchronizes the Go Task bootstrap dependency with the version supplied by the Nix
# development shell. Nix is the authoritative Task version source.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

NIX_TASK_VERSION="$(nix develop --command task --version)"
if ! [[ "$NIX_TASK_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: failed to parse Task version from Nix: $NIX_TASK_VERSION" >&2
  exit 1
fi

(
  cd tools/external
  GOWORK=off go get -tool "github.com/go-task/task/v3/cmd/task@v$NIX_TASK_VERSION"
  GOWORK=off go mod tidy
)
./scripts/sync_task_checksums.sh \
  "v${NIX_TASK_VERSION}" \
  "https://github.com/go-task/task/releases/download/v${NIX_TASK_VERSION}/task_checksums.txt" \
  scripts/setup_task.sh

echo "synchronized tools/external/go.mod and Task archive checksums to Task $NIX_TASK_VERSION"
