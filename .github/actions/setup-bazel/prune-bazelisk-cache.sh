#!/usr/bin/env bash

set -euo pipefail

# Remove old releases so prefix restores do not carry them into new cache entries.
bazelisk_home="${1:?missing Bazelisk home}"
bazelisk_cache_dir="$(dirname "$bazelisk_home")"
bazelisk_version="$(basename "$bazelisk_home")"

find "$bazelisk_cache_dir" -mindepth 1 -maxdepth 1 -type d ! -name "$bazelisk_version" -exec rm -rf {} +
