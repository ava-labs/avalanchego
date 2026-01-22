#!/usr/bin/env bash
#
# Creates release tags for avalanchego and its submodules.
#
# Usage: ./scripts/create_release_tags.sh <version>
#
# Example:
#   ./scripts/create_release_tags.sh v1.15.0
#   ./scripts/create_release_tags.sh v0.0.0-mytest

set -euo pipefail

VERSION="${1:?Usage: create_release_tags.sh <version>}"

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

echo "Creating tags for $VERSION at $(git rev-parse --short HEAD):"
for tag in "${TAGS[@]}"; do
    echo "  $tag"
    git tag "$tag"
done

echo ""
echo "Push with: ./scripts/run_task.sh tags-push -- $VERSION"
