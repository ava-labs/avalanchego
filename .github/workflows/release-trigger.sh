#!/usr/bin/env bash
# release-trigger.sh — dispatch the release workflow for TAG.
#
# Usage: release-trigger.sh <TAG>
#
# The release is dispatch-driven rather than tag-push-driven. The release
# procedure pushes four tags at once (root + three graft modules), and GitHub
# creates no tag push events when more than three tags are pushed together
# (https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#push),
# so a `push: tags` trigger on release.yml would silently never fire.
# Dispatching explicitly also keeps a release from waking the repo's wildcard
# `tags: "*"` workflows (CI, Bazel, buf, Docker publish), which the four-tag
# push otherwise leaves suppressed.
#
# Run this AFTER the tags exist on the remote (`task tags-push -- <TAG>`).
#
# Requires:
#   - `gh` authenticated with permission to dispatch workflows
#   - GH_REPO set, or a checkout whose origin remote is the target repo
#   - release.yml present on the repo's default branch (it defines the
#     workflow_dispatch input this dispatches against)

set -euo pipefail

TAG="${1:?Usage: release-trigger.sh <TAG>}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=.github/workflows/release-lib.sh
source "${HERE}/release-lib.sh"

if [[ ! "$TAG" =~ $SEMVER_REGEX ]]; then
    echo "Error: '$TAG' is not a valid release tag (expected vX.Y.Z or vX.Y.Z-suffix)." >&2
    exit 1
fi

echo "Dispatching release workflow for ${TAG}..."
gh workflow run release.yml --field tag="${TAG}"

echo ""
echo "Dispatched. Watch the run with:"
echo "  gh run list --workflow=release.yml --limit 5"
echo "  gh run watch \"\$(gh run list --workflow=release.yml --limit 1 --json databaseId --jq '.[0].databaseId')\""
