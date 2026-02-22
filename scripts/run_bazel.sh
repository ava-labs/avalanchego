#!/usr/bin/env bash

set -euo pipefail

if command -v bazelisk > /dev/null 2>&1; then
  exec bazelisk "${@}"
elif command -v nix > /dev/null 2>&1; then
  exec nix run nixpkgs#bazelisk -- "${@}"
else
  echo "Error: bazelisk not found. Install via 'nix develop' or see docs/bazel.md" >&2
  exit 1
fi
