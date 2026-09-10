#!/usr/bin/env bash
# lib_version.sh — shared version-validation constants.
#
# Source from another script after resolving the repo root, e.g.:
#   REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
#   # shellcheck source=scripts/lib_version.sh
#   source "${REPO_ROOT}/scripts/lib_version.sh"

# Canonical semver regex for avalanchego release tags.
# Matches vX.Y.Z or vX.Y.Z-suffix.
# shellcheck disable=SC2034  # consumed by sourcing scripts, not in this file
readonly SEMVER_REGEX='^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$'
