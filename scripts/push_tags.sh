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

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    sed -n '2,/^$/{ s/^# \?//; p }' "$0"
    exit 0
fi

VERSION="${1:?Usage: push_tags.sh <version>}"
REMOTE="${GIT_REMOTE:-origin}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib_version.sh
source "$REPO_ROOT/scripts/lib_version.sh"

if [[ ! "$VERSION" =~ $SEMVER_REGEX ]]; then
    echo "Error: Version must match vX.Y.Z or vX.Y.Z-suffix" >&2
    exit 1
fi

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
# --atomic: all tags land or none do, so a partial push can't leave the remote
# with an inconsistent subset of the release's coordinated tags.
git push --atomic "$REMOTE" "${TAGS[@]}"
