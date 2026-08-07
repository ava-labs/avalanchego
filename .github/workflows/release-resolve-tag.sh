#!/usr/bin/env bash
# release-resolve-tag.sh — resolve the release tag from the producer/umbrella
# input or fall back to GITHUB_REF.
#
# Usage: TAG_INPUT=<maybe-empty> release-resolve-tag.sh
#
# Reads:
#   TAG_INPUT   — set by workflow_call/workflow_dispatch (preferred when non-empty)
#   GITHUB_REF  — set by Actions runner; e.g. "refs/tags/v1.14.3" on push:tags
#
# Prints the resolved tag on stdout. Exits 1 when TAG_INPUT is empty AND
# GITHUB_REF is unset (e.g. invoked outside Actions with no input).

set -euo pipefail

if [[ -n "${TAG_INPUT:-}" ]]; then
  printf '%s\n' "$TAG_INPUT"
  exit 0
fi
if [[ -z "${GITHUB_REF:-}" ]]; then
  echo "Error: TAG_INPUT empty and GITHUB_REF not set" >&2
  exit 1
fi
printf '%s\n' "${GITHUB_REF#refs/tags/}"
