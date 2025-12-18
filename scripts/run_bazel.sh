#!/usr/bin/env bash
# Wrapper script to run bazel inside a nix shell.
# Used by Taskfile to ensure consistent build environment.

set -euo pipefail

# Check if we're already in a nix shell
if [[ -n "${IN_NIX_SHELL:-}" ]] || [[ -n "${NIX_BUILD_TOP:-}" ]]; then
    # Already in nix shell, run bazel directly
    exec bazel "$@"
fi

# Check if nix is available
if ! command -v nix &> /dev/null; then
    echo "ERROR: nix is not installed." >&2
    echo "Install nix: task install-nix" >&2
    exit 1
fi

# Not in nix shell - run bazel inside nix develop
echo "Not in nix shell, running bazel inside 'nix develop'..." >&2
exec nix develop --command bazel "$@"
