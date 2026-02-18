#!/usr/bin/env bash
#
# Creates release tags for avalanchego and its submodules.
#
# Usage: ./scripts/create_tags.sh [--no-sign] <version>
#
# Tags are signed by default (requires GPG key).
#
# Options:
#   --no-sign    Create unsigned tags (e.g., for development tags)
#
# Example:
#   ./scripts/create_tags.sh v1.15.0
#   ./scripts/create_tags.sh --no-sign v0.0.0-mytest

set -euo pipefail

SIGN_FLAG="-s"
if [[ "${1:-}" == "--no-sign" ]]; then
    SIGN_FLAG=""
    shift
fi

VERSION="${1:?Usage: create_tags.sh [--no-sign] <version>}"

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

HEAD_COMMIT="$(git rev-parse HEAD)"
HEAD_SHORT="$(git rev-parse --short HEAD)"

# Check for existing tags before creating any
existing=()
all_at_head=true
for tag in "${TAGS[@]}"; do
    if tag_commit="$(git rev-parse "$tag" 2>/dev/null)"; then
        existing+=("$tag")
        if [[ "$tag_commit" != "$HEAD_COMMIT" ]]; then
            all_at_head=false
        fi
    fi
done

if [[ ${#existing[@]} -gt 0 ]]; then
    if [[ ${#existing[@]} -eq ${#TAGS[@]} && "$all_at_head" == true ]]; then
        echo "All tags for $VERSION already exist at HEAD ($HEAD_SHORT)."
        echo ""
        echo "Push with: ./scripts/run_task.sh tags-push -- $VERSION"
        exit 0
    fi

    echo "Error: Some tags for $VERSION already exist (HEAD is $HEAD_SHORT):" >&2
    for tag in "${existing[@]}"; do
        echo "  $tag -> $(git rev-parse --short "$tag")" >&2
    done
    echo "" >&2
    echo "To delete and re-create:" >&2
    echo "  git tag -d ${existing[*]}" >&2
    exit 1
fi

# Create all tags
echo "Creating tags for $VERSION at $HEAD_SHORT:"
for tag in "${TAGS[@]}"; do
    echo "  $tag"
    git tag $SIGN_FLAG "$tag"
done

echo ""
echo "Push with: ./scripts/run_task.sh tags-push -- $VERSION"
