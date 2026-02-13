#!/usr/bin/env bash
#
# Pushes release tags for avalanchego and its submodules.
#
# Usage: ./scripts/push_tags.sh <version>
#
# Environment:
#   GIT_REMOTE - Remote to push to (default: origin)
#
# Example:
#   ./scripts/push_tags.sh v1.15.0
#   GIT_REMOTE=upstream ./scripts/push_tags.sh v0.0.0-mytest

set -euo pipefail

VERSION="${1:?Usage: push_tags.sh <version>}"
REMOTE="${GIT_REMOTE:-origin}"

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$ ]]; then
    echo "Error: Version must match vX.Y.Z or vX.Y.Z-suffix" >&2
    exit 1
fi

TAGS=(
    "$VERSION"
    "graft/evm/$VERSION"
    "graft/coreth/$VERSION"
    "graft/subnet-evm/$VERSION"
)

echo "Pushing tags for $VERSION to $REMOTE:"
git push "$REMOTE" "${TAGS[@]}"
