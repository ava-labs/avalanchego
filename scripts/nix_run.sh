#!/usr/bin/env bash

set -euo pipefail

# This wrapper keeps task entrypoints repo-defined without forcing nested
# `nix develop` shells when the caller is already inside the dev environment.
# Tasks should use repo-provided tools for reproducibility; users who want a
# custom toolchain can still invoke commands directly from their own shell.
#
# Behavior:
# - inside `nix develop`: exec the command directly
# - outside `nix develop`: re-enter the repo dev shell and exec the command there

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ $# -eq 0 ]]; then
  echo "Usage: $0 <command> [args...]" >&2
  exit 1
fi

if [[ -n "${IN_NIX_SHELL-}" ]]; then
  exec "$@"
fi

if ! command -v nix > /dev/null 2>&1; then
  exec "$@"
fi

exec nix develop "${REPO_ROOT}" --command "$@"
