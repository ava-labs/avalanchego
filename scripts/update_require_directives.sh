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
    for module in "${MODULE_PATHS[@]}"; do
        # Only update if the module is already required
        # Escape dots for regex and match module followed by whitespace (space or tab)
        module_pattern="${module//./\\.}"
        if grep -qE "[[:space:]]${module_pattern}[[:space:]]" "$go_mod" 2>/dev/null; then
            go mod edit -require="${module}@${VERSION}" "$go_mod"
        fi
    done
done
