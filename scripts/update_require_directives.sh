#!/usr/bin/env bash
#
# Updates require directives for avalanchego modules in all go.mod files.
#
# Usage: ./scripts/update_require_directives.sh <version>
#
# Example:
#   ./scripts/update_require_directives.sh v1.15.0
#   ./scripts/update_require_directives.sh v0.0.0-mytest

set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    sed -n '2,/^$/{ s/^# \?//; p }' "$0"
    exit 0
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$REPO_ROOT/scripts/lib_go_modules.sh"

VERSION="${1:?Usage: update_require_directives.sh <version>}"

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$ ]]; then
    echo "Error: Version must match vX.Y.Z or vX.Y.Z-suffix" >&2
    exit 1
fi

echo "Updating require directives to $VERSION"
for go_mod in "${GO_MODS[@]}"; do
    echo "  $go_mod"
    # Parse requires from go.mod and update any that reference internal modules
    requires=$(go mod edit -json "$go_mod" | jq -r '.Require[]? | .Path')
    for module in "${MODULE_PATHS[@]}"; do
        if echo "$requires" | grep -qxF "$module"; then
            go mod edit -require="${module}@${VERSION}" "$go_mod"
        fi
    done
done
