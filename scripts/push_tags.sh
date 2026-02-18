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

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$REPO_ROOT/scripts/lib_go_modules.sh"

TAGS=()
for prefix in "${TAG_PREFIXES[@]}"; do
    TAGS+=("${prefix}${VERSION}")
done

# Verify all tags exist locally before pushing
for tag in "${TAGS[@]}"; do
    if ! git rev-parse "$tag" >/dev/null 2>&1; then
        echo "Error: Tag '$tag' does not exist locally." >&2
        echo "Run './scripts/run_task.sh tags-create -- $VERSION' first." >&2
        exit 1
    fi
done

echo "Pushing tags for $VERSION to $REMOTE:"
git push "$REMOTE" "${TAGS[@]}"
