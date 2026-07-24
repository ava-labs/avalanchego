#!/usr/bin/env bash
# release-resolve-previous-tag.sh — call release-previous-tag.sh and translate
# its tri-state exit code into a single value suitable for the workflow.
#
# Usage: release-resolve-previous-tag.sh <TAG> [RELEASES_MD_PATH]
#
# Output:
#   - Exit 0, stdout = previous tag, when the upstream helper succeeds.
#   - Exit 0, stdout = empty,        when <TAG> is the oldest entry (upstream exit 1).
#   - Exit 2, no output,             when <TAG> is not in RELEASES.md (upstream exit 2).
#
# This collapses the "previous_tag may be empty" case into normal success so
# the publish job's step stays a trivial assign-and-emit block, while the
# fatal not-found case (exit 2) still fails the step.

set -euo pipefail

TAG="${1:?Usage: release-resolve-previous-tag.sh <TAG> [RELEASES_MD_PATH]}"
RELEASES_MD="${2:-RELEASES.md}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

rc=0
prev="$("${HERE}/release-previous-tag.sh" "$TAG" "$RELEASES_MD")" || rc=$?

case "$rc" in
  0) printf '%s\n' "$prev" ;;
  1) printf '\n' ;; # <TAG> is the oldest entry — no previous tag, but not an error.
  *) exit "$rc" ;;  # 2 (not found) or any other failure — propagate.
esac
