#!/usr/bin/env bash

set -euo pipefail

# Checks that Rust version and edition in MODULE.bazel match firewood/Cargo.toml.

if ! [[ "$0" =~ scripts/check_rust_version.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

cargo_toml="firewood/Cargo.toml"
bazel_file="MODULE.bazel"

# Extract rust-version from firewood/Cargo.toml (workspace.package section)
cargo_version=$(sed -n 's/^rust-version = "\(.*\)"$/\1/p' "$cargo_toml")
if [[ -z "$cargo_version" ]]; then
  echo "error: failed to parse rust-version from $cargo_toml"
  exit 1
fi

# Extract edition from firewood/Cargo.toml (workspace.package section)
cargo_edition=$(sed -n 's/^edition = "\(.*\)"$/\1/p' "$cargo_toml")
if [[ -z "$cargo_edition" ]]; then
  echo "error: failed to parse edition from $cargo_toml"
  exit 1
fi

# Extract version from MODULE.bazel rust.toolchain()
bazel_version=$(sed -n 's/^[[:space:]]*versions = \["\(.*\)"\],$/\1/p' "$bazel_file")
if [[ -z "$bazel_version" ]]; then
  echo "error: failed to parse rust.toolchain versions from $bazel_file"
  exit 1
fi

# Extract edition from MODULE.bazel rust.toolchain()
bazel_edition=$(sed -n 's/^[[:space:]]*edition = "\(.*\)",$/\1/p' "$bazel_file")
if [[ -z "$bazel_edition" ]]; then
  echo "error: failed to parse rust.toolchain edition from $bazel_file"
  exit 1
fi

mismatches=()

if [[ "$bazel_version" != "$cargo_version" ]]; then
  mismatches+=("rust-version: $cargo_toml=$cargo_version, $bazel_file=$bazel_version")
fi

if [[ "$bazel_edition" != "$cargo_edition" ]]; then
  mismatches+=("edition: $cargo_toml=$cargo_edition, $bazel_file=$bazel_edition")
fi

if [[ ${#mismatches[@]} -gt 0 ]]; then
  echo "Rust version mismatch (reference: $cargo_toml):"
  for m in "${mismatches[@]}"; do
    echo "  $m"
  done
  exit 1
fi

echo "Rust version and edition are consistent: $cargo_version (edition $cargo_edition)"
