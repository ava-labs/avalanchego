#!/usr/bin/env bash
# release-classify-tag.sh — classify a release tag as stable or pre-release.
#
# Usage: release-classify-tag.sh <TAG>
#
# Validates that <TAG> matches the canonical SEMVER_REGEX (vX.Y.Z[-suffix])
# and prints "prerelease" if the tag has a suffix, "stable" otherwise.
# Exits non-zero on invalid input.

set -euo pipefail

TAG="${1:?Usage: release-classify-tag.sh <TAG>}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=.github/workflows/release-lib.sh
source "${HERE}/release-lib.sh"

if [[ ! "$TAG" =~ $SEMVER_REGEX ]]; then
  echo "Error: tag '${TAG}' does not match vX.Y.Z(-suffix)?" >&2
  exit 1
fi

if [[ -n "${BASH_REMATCH[1]:-}" ]]; then
  echo "prerelease"
else
  echo "stable"
fi
