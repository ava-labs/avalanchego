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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

VERSION="${1:?Usage: update_require_directives.sh <version>}"

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$ ]]; then
    echo "Error: Version must match vX.Y.Z or vX.Y.Z-suffix" >&2
    exit 1
fi

cd "$REPO_ROOT"

# Module paths to update
declare -a MODULES=(
    "github.com/ava-labs/avalanchego"
    "github.com/ava-labs/avalanchego/graft/evm"
    "github.com/ava-labs/avalanchego/graft/coreth"
    "github.com/ava-labs/avalanchego/graft/subnet-evm"
)

GO_MODS=(
    "go.mod"
    "graft/evm/go.mod"
    "graft/coreth/go.mod"
    "graft/subnet-evm/go.mod"
)

echo "Updating require directives to $VERSION"
for go_mod in "${GO_MODS[@]}"; do
    echo "  $go_mod"
    for module in "${MODULES[@]}"; do
        # Only update if the module is already required
        if grep -qF "$module " "$go_mod" 2>/dev/null; then
            go mod edit -require="${module}@${VERSION}" "$go_mod"
        fi
    done
done
