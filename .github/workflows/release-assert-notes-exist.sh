#!/usr/bin/env bash
# release-assert-notes-exist.sh — fail loudly if RELEASES.md has no section
# for the pushed tag.
#
# Usage: release-assert-notes-exist.sh <TAG> [RELEASES_MD_PATH]
#
# Runs release-extract-notes.sh on <TAG> and checks the exit code. If the
# helper exits non-zero (no matching `## Pending ($TAG)` or `## [$TAG](url)`
# section), prints a multi-line diagnostic explaining what's expected, then
# exits 1.

set -euo pipefail

TAG="${1:?Usage: release-assert-notes-exist.sh <TAG> [RELEASES_MD_PATH]}"
RELEASES_MD="${2:-RELEASES.md}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! "${HERE}/release-extract-notes.sh" "$TAG" "$RELEASES_MD" >/dev/null 2>&1; then
  echo "Error: ${RELEASES_MD} does not contain a section for tag '${TAG}'." >&2
  echo "Expected one of these level-2 headers (with a non-empty body):" >&2
  echo "  ## Pending (${TAG})" >&2
  echo "  ## [${TAG}](https://github.com/ava-labs/avalanchego/releases/tag/${TAG})" >&2
  echo "Add the section (header + body) to ${RELEASES_MD} and re-tag." >&2
  exit 1
fi
echo "Found a ${RELEASES_MD} section for ${TAG}."
