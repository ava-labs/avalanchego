#!/usr/bin/env bash
#
# Checks that internal module require directives are consistent across all
# go.mod files. Every require of an avalanchego submodule must reference the
# same version.
#
# See docs/design/multi-module-release.md for background.

set -euo pipefail

if ! [[ "$0" =~ scripts/check_require_directives.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Discover all go.mod files tracked by git
mapfile -t GO_MODS < <(git ls-files 'go.mod' '*/go.mod')

# Build the set of internal module paths from the go.mod files themselves
declare -A internal_modules
for go_mod in "${GO_MODS[@]}"; do
  mod_path=$(go mod edit -json "$go_mod" | jq -r '.Module.Path')
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
  echo "error: no internal module require directives found"
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
  echo "Inconsistent internal module require versions (expected $reference):"
  echo "  ${sources[0]}"
  for m in "${mismatches[@]}"; do
    echo "  $m"
  done
  echo ""
  echo "Run './scripts/run_task.sh tags-set-require-directives -- <version>' to fix."
  exit 1
fi

echo "All internal module require directives are consistent: $reference"
