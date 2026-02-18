#!/usr/bin/env bash
#
# Verifies that release tags for avalanchego and its submodules exist on the
# remote. Intended to run after push_tags.sh to confirm delivery.
#
# Usage: ./scripts/verify_tags_remote.sh <version>
#
# Environment:
#   GIT_REMOTE - Remote to check (default: origin)
#
# Example:
#   ./scripts/verify_tags_remote.sh v1.15.0
#   GIT_REMOTE=upstream ./scripts/verify_tags_remote.sh v0.0.0-mytest

set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    sed -n '2,/^$/{ s/^# \?//; p }' "$0"
    exit 0
fi

VERSION="${1:?Usage: verify_tags_remote.sh <version>}"
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

echo "Verifying tags for $VERSION on $REMOTE:"
remote_refs=$(git ls-remote --tags "$REMOTE" | awk '{print $2}') # vcs-ok: git ls-remote --tags has no jj equivalent

missing=()
for tag in "${TAGS[@]}"; do
    ref="refs/tags/$tag"
    if echo "$remote_refs" | grep -qxF "$ref"; then
        echo "  $tag ✓"
    else
        echo "  $tag ✗ (missing)" >&2
        missing+=("$tag")
    fi
done

if [[ ${#missing[@]} -gt 0 ]]; then
    echo "" >&2
    echo "Error: ${#missing[@]} tag(s) missing on $REMOTE." >&2
    echo "Re-run: ./scripts/run_task.sh tags-push -- $VERSION" >&2
    exit 1
fi

echo ""
echo "All tags for $VERSION verified on $REMOTE."
