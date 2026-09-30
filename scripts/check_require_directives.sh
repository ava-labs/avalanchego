#!/usr/bin/env bash
#
# Checks that internal module require directives are consistent across all
# go.mod files, and that they match version.Current and the top section of
# RELEASES.md. Development tags (v0.0.0-*) are exempt from both checks.
#
# See docs/design/multi-module-release.md for background.

set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    sed -n '2,/^$/{ s/^# \?//; p }' "$0"
    exit 0
fi

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

# Development tags share work-in-progress from a branch, so they don't name the
# version being developed
if [[ "$reference" == v0.0.0-* ]]; then
  echo "All internal module require directives reference development tag: $reference"
  exit 0
fi

# Check the require version matches the version being developed.
current_version=$(<"$REPO_ROOT/version/current.txt")

if [[ "$current_version" != "$reference" ]]; then
  echo "Internal module require version $reference does not match version/current.txt $current_version" >&2
  echo "" >&2
  echo "If version.Current changed, run './scripts/run_task.sh tags-update-require-directives -- $current_version'." >&2
  echo "Otherwise, set version.Current and version/current.txt to $reference." >&2
  exit 1
fi

expected_heading="## [$reference](https://github.com/ava-labs/avalanchego/releases/tag/$reference)"
actual_heading=$(grep -m1 '^## ' "$REPO_ROOT/RELEASES.md" || true)

if [[ "$actual_heading" != "$expected_heading" ]]; then
  echo "The first section of RELEASES.md must be the next release:" >&2
  echo "  expected: $expected_heading" >&2
  echo "  actual:   $actual_heading" >&2
  exit 1
fi

echo "All internal module require directives match version.Current and RELEASES.md: $reference"
