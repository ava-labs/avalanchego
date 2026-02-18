#!/usr/bin/env bash
#
# Checks that internal module require directives are consistent across all
# go.mod files. Every require of an avalanchego submodule must reference the
# same version.
#
# See docs/design/multi-module-release.md for background.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$REPO_ROOT/scripts/lib_go_modules.sh"

# Build the set of internal module paths from the discovered modules
declare -A internal_modules
for mod_path in "${MODULE_PATHS[@]}"; do
  internal_modules["$mod_path"]=1
done

# Collect all (version, source) pairs for internal module requires
declare -a versions=()
declare -a sources=()

for go_mod in "${GO_MODS[@]}"; do
  requires=$(go mod edit -json "$go_mod" | jq -r '.Require[]? | "\(.Path) \(.Version)"')

  while IFS=' ' read -r mod_path version; do
    if [[ -n "$mod_path" && -n "${internal_modules[$mod_path]+x}" ]]; then
      versions+=("$version")
      sources+=("$go_mod: $mod_path@$version")
    fi
  done <<< "$requires"
done

if [[ ${#versions[@]} -eq 0 ]]; then
  echo "error: no internal module require directives found" >&2
  exit 1
fi

# Check all versions are the same
reference="${versions[0]}"
mismatches=()

for i in "${!versions[@]}"; do
  if [[ "${versions[$i]}" != "$reference" ]]; then
    mismatches+=("${sources[$i]}")
  fi
done

if [[ ${#mismatches[@]} -gt 0 ]]; then
  echo "Inconsistent internal module require versions (expected $reference):" >&2
  echo "  ${sources[0]}" >&2
  for m in "${mismatches[@]}"; do
    echo "  $m" >&2
  done
  echo "" >&2
  echo "Run './scripts/run_task.sh tags-update-require-directives -- <version>' to fix." >&2
  exit 1
fi

echo "All internal module require directives are consistent: $reference"
