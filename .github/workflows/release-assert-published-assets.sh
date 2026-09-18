#!/usr/bin/env bash
# release-assert-published-assets.sh — assert the GitHub release's asset
# set exactly matches the canonical manifest.
#
# Usage: release-assert-published-assets.sh <TAG>
#
# Reads:
#   GH_REPO or GITHUB_REPOSITORY
#   GH_TOKEN  (for `gh api`)
#
# Uses the LISTING endpoint, not the tag-keyed endpoint, so drafts created
# by the publish job are visible (the tag-keyed endpoint excludes drafts).
# Fails (exit 1) with a unified diff on any drift; fails (exit 1) with a
# distinct message if no release exists for the tag — softprops may have
# silently no-op'd.

set -euo pipefail

TAG="${1:?Usage: release-assert-published-assets.sh <TAG>}"
REPO="${GH_REPO:-${GITHUB_REPOSITORY:-}}"
if [[ -z "$REPO" ]]; then
  echo "Error: GH_REPO or GITHUB_REPOSITORY must be set" >&2
  exit 1
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=.github/workflows/release-lib.sh
source "${HERE}/release-lib.sh"

expected_file="$(mktemp)"
published_file="$(mktemp)"
trap 'rm -f "$expected_file" "$published_file"' EXIT

"${HERE}/release-expected-manifest.sh" "$TAG" | sort -u > "$expected_file"

gh api --paginate "repos/${REPO}/releases?per_page=100" \
    --jq ".[] | select(.tag_name == \"${TAG}\") | .assets[].name" \
  | sort -u > "$published_file"

if [[ ! -s "$published_file" ]]; then
  echo "Error: no release (draft or published) found for tag '${TAG}'." >&2
  echo "softprops/action-gh-release may have silently failed; investigate." >&2
  exit 1
fi

assert_set_equals_manifest "$expected_file" "$published_file" "published release assets"
echo "Published release for ${TAG} has exactly the expected asset set."
