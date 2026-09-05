#!/usr/bin/env bash

set -euo pipefail

# Synchronizes the Go Task bootstrap dependency with the version supplied by the Nix
# development shell. Nix is the authoritative Task version source.

if ! [[ "$0" =~ scripts/sync_task_version.sh ]]; then
  echo "must be run from repository root" >&2
  exit 255
fi

nix_task_version="$(task --version)"
if ! [[ "$nix_task_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: failed to parse Task version from Nix: $nix_task_version" >&2
  exit 1
fi

(
  cd tools/external
  GOWORK=off go get -tool "github.com/go-task/task/v3/cmd/task@v$nix_task_version"
  GOWORK=off go mod tidy
)

echo "synchronized tools/external/go.mod to Task $nix_task_version"
