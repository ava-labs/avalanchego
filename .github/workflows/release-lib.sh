#!/usr/bin/env bash
# release-lib.sh — shared functions for release-*.sh helpers.
#
# Sourced by other release-*.sh scripts; not invoked directly.
# Re-exports SEMVER_REGEX from scripts/lib_version.sh so the release pipeline
# uses the same canonical regex as the tag-management scripts.

# Resolve repo root by going up two levels from this file's directory
# (.github/workflows/ → .github/ → repo root).
LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${LIB_DIR}/../.." && pwd)"
# shellcheck source=scripts/lib_version.sh
source "${REPO_ROOT}/scripts/lib_version.sh"

# assert_set_equals_manifest <expected_file> <actual_file> <description>
#   Compares two newline-delimited sorted-and-deduped sets. Prints a unified
#   diff and returns 1 on mismatch. Used by the "set equality" gates in the
#   publish job (publish-set vs manifest, published assets vs manifest).
#   <description> is a human-readable noun phrase for the error line
#   (e.g. "publish-set").
assert_set_equals_manifest() {
  local expected_file="$1"
  local actual_file="$2"
  local description="$3"
  if ! diff -u "$expected_file" "$actual_file" >/dev/null; then
    echo "Error: ${description} does not match manifest." >&2
    diff -u "$expected_file" "$actual_file" >&2 || true
    return 1
  fi
}
