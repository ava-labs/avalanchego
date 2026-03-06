#!/usr/bin/env bash

set -euo pipefail

# Wrapper for CI and automation. For local development, bazelisk is
# available in the nix dev shell (see flake.nix) or via direnv when
# AVALANCHEGO_DIRENV_USE_FLAKE is set (see .envrc). Regular users can
# also install bazelisk directly and alias it to `bazel`.
# See docs/bazel.md "Prerequisites" for details.

if command -v bazelisk > /dev/null 2>&1; then
  exec bazelisk "${@}"
elif command -v nix > /dev/null 2>&1; then
  REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  exec nix develop "${REPO_ROOT}" --command bazelisk "${@}"
else
  echo "Error: bazelisk not found. Install via 'nix develop' or see docs/bazel.md" >&2
  exit 1
fi
