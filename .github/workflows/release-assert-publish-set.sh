#!/usr/bin/env bash
# release-assert-publish-set.sh — assert the local publish-set exactly
# matches the canonical manifest.
#
# Usage: release-assert-publish-set.sh <TAG> <DIR>
#
# Compares the basenames of files under DIR (max-depth 1) against the
# manifest emitted by release-expected-manifest.sh. Fails (exit 1) with a
# unified diff on any mismatch.

set -euo pipefail

TAG="${1:?Usage: release-assert-publish-set.sh <TAG> <DIR>}"
DIR="${2:?Usage: release-assert-publish-set.sh <TAG> <DIR>}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=.github/workflows/release-lib.sh
source "${HERE}/release-lib.sh"

expected_file="$(mktemp)"
actual_file="$(mktemp)"
trap 'rm -f "$expected_file" "$actual_file"' EXIT

"${HERE}/release-expected-manifest.sh" "$TAG" | sort -u > "$expected_file"
find "$DIR" -maxdepth 1 -type f -exec basename {} \; | sort -u > "$actual_file"

assert_set_equals_manifest "$expected_file" "$actual_file" "publish-set"
echo "Publish-set matches manifest exactly."
