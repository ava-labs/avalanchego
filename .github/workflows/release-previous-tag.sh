#!/usr/bin/env bash
# release-previous-tag.sh — find the previous tag in RELEASES.md.
#
# Usage: release-previous-tag.sh <TAG> [RELEASES_MD_PATH]
#
# Scans the file (top-to-bottom = newest-to-oldest) for the section header
# matching <TAG>, then prints the version tag from the very next ## header.
# Exit codes:
#   0  — previous tag printed
#   1  — <TAG> found but no preceding entry (it's the oldest in the file)
#   2  — <TAG> not found in the file at all

set -euo pipefail

TAG="${1:?Usage: release-previous-tag.sh <TAG> [RELEASES_MD_PATH]}"
RELEASES_MD="${2:-RELEASES.md}"

awk -v tag="$TAG" '
  /^## / {
    if (after) {
      if (match($0, /[([]v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?[)\]]/)) {
        prev = substr($0, RSTART+1, RLENGTH-2)
        print prev
        found = 1
        exit
      }
      next
    }
    if (index($0, "(" tag ")") || index($0, "[" tag "]")) {
      after = 1
    }
  }
  END {
    if (found) exit 0   # success path — previous tag was printed
    if (!after) exit 2  # target tag not found at all
    exit 1              # target tag found but no preceding entry
  }
' "$RELEASES_MD"
