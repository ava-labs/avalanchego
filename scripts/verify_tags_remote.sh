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

echo "Verifying tags for $VERSION on $REMOTE:"
remote_ls="$(git ls-remote --tags "$REMOTE")"

# Resolve a tag to the commit it points at: prefer the peeled (^{}) line that
# annotated tags emit; fall back to the ref line for lightweight tags.
tag_commit() {
    local tag="$1"
    awk -v peeled="refs/tags/${tag}^{}" -v plain="refs/tags/${tag}" '
        $2==peeled {p=$1} $2==plain {r=$1}
        END { if (p!="") print p; else print r }
    ' <<<"$remote_ls"
}

missing=()
commits=()
for i in "${!TAGS[@]}"; do
    tag="${TAGS[$i]}"
    commit="$(tag_commit "$tag")"
    commits[i]="$commit"
    if [[ -z "$commit" ]]; then
        echo "  $tag ✗ (missing)" >&2
        missing+=("$tag")
    else
        echo "  $tag ✓ ${commit}"
    fi
done

if [[ ${#missing[@]} -gt 0 ]]; then
    echo "" >&2
    echo "Error: ${#missing[@]} tag(s) missing on $REMOTE." >&2
    echo "Re-run: ./scripts/run_task.sh tags-push -- $VERSION" >&2
    exit 1
fi

# All release tags must point at the same commit. create_tags.sh tags a single
# commit, so a stale or non-atomically pushed module tag resolving elsewhere
# would hand Go-module consumers a version that disagrees with the root release.
root_commit="${commits[0]}"
divergent=()
for i in "${!TAGS[@]}"; do
    if [[ "${commits[$i]}" != "$root_commit" ]]; then
        divergent+=("${TAGS[$i]} -> ${commits[$i]}")
    fi
done

if [[ ${#divergent[@]} -gt 0 ]]; then
    echo "" >&2
    echo "Error: release tags for $VERSION point at different commits on $REMOTE (expected all at ${root_commit}):" >&2
    printf '  %s\n' "${divergent[@]}" >&2
    exit 1
fi

echo ""
echo "All tags for $VERSION verified on $REMOTE (commit ${root_commit})."
