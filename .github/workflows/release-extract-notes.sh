#!/usr/bin/env bash
# release-extract-notes.sh — extract a release section from RELEASES.md.
#
# Usage: release-extract-notes.sh <TAG> [RELEASES_MD_PATH]
#
# Prints the body of the markdown section whose header contains the tag
# (matches "## Pending (vX.Y.Z)" or "## [vX.Y.Z](...)" forms). Trims
# leading/trailing blank lines. Exits 1 if no section matches OR if the
# matched section has only a header (no body) — a header-only section is
# treated as "no notes" so the release-notes preflight gate fails fast
# rather than letting a draft ship with only the "Previous Tag:" prefix.

set -euo pipefail

TAG="${1:?Usage: release-extract-notes.sh <TAG> [RELEASES_MD_PATH]}"
RELEASES_MD="${2:-RELEASES.md}"

if [[ ! -f "$RELEASES_MD" ]]; then
  echo "Error: ${RELEASES_MD} not found" >&2
  exit 1
fi

awk -v tag="$TAG" '
  /^## / {
    if (in_section) exit       # next section header — stop collecting
    if (index($0, "(" tag ")") || index($0, "[" tag "]")) {
      in_section = 1
      next
    }
    next                       # other ## headers (different versions) — skip
  }
  in_section { lines[++n] = $0 }
  END {
    if (!in_section) exit 1
    start = 1
    while (start <= n && lines[start] == "") start++
    end = n
    while (end >= start && lines[end] == "") end--
    # A section with only a header (or only blank lines below it) leaves
    # start > end after trimming. Without this guard the script would exit
    # 0 with no output, which downstream callers treat as success — the
    # release-notes preflight gate would pass and the draft release would
    # ship with just the "Previous Tag:" prefix and no actual notes.
    if (start > end) exit 1
    for (i = start; i <= end; i++) print lines[i]
  }
' "$RELEASES_MD"
