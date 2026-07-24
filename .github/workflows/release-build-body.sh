#!/usr/bin/env bash
# release-build-body.sh — emit the full release body for <TAG>.
#
# Usage: release-build-body.sh <TAG> <PREV_TAG_OR_EMPTY> [RELEASES_MD_PATH]
#
# Writes to stdout:
#   - Optional "Previous Tag: ..." line (omitted when PREV_TAG is empty)
#   - Blank line
#   - The RELEASES.md section body for <TAG> (via release-extract-notes.sh)

set -euo pipefail

TAG="${1:?Usage: release-build-body.sh <TAG> <PREV_TAG_OR_EMPTY> [RELEASES_MD_PATH]}"
PREV_TAG="${2-}"
RELEASES_MD="${3:-RELEASES.md}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -n "$PREV_TAG" ]]; then
  printf 'Previous Tag: [%s](https://github.com/ava-labs/avalanchego/releases/tag/%s)\n\n' \
    "$PREV_TAG" "$PREV_TAG"
fi

"${HERE}/release-extract-notes.sh" "$TAG" "$RELEASES_MD"
