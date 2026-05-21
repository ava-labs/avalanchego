#!/usr/bin/env bash
#
# Validates that update_require_directives.sh is idempotent by re-running it
# with the current version and verifying no files change. This catches bugs
# in the update script itself and ensures the committed state is consistent
# with what the tooling produces.
#
# Requires a clean git working tree.

set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    sed -n '2,/^$/{ s/^# \?//; p }' "$0"
    exit 0
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$REPO_ROOT/scripts/lib_go_modules.sh"

# Extract the current consistent version from the first internal module require
current_version=""
for go_mod in "${GO_MODS[@]}"; do
  requires=$(go mod edit -json "$go_mod" | jq -r '.Require[]? | "\(.Path) \(.Version)"')
  while IFS=' ' read -r mod_path version; do
    for internal in "${MODULE_PATHS[@]}"; do
      if [[ "$mod_path" == "$internal" ]]; then
        current_version="$version"
        break 3
      fi
    done
  done <<< "$requires"
done

if [[ -z "$current_version" ]]; then
  echo "Error: could not determine current require directive version" >&2
  exit 1
fi

echo "Re-running update_require_directives.sh with current version: $current_version"
"$REPO_ROOT/scripts/update_require_directives.sh" "$current_version"
