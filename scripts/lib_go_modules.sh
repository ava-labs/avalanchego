#!/usr/bin/env bash
#
# Shared module definitions for avalanchego multi-module scripts.
#
# Provides:
#   GO_MODS        - Array of go.mod file paths
#   MODULE_PATHS   - Array of module paths
#   TAG_PREFIXES   - Array of tag prefixes
#
# TAG_PREFIXES is the canonical list. GO_MODS and MODULE_PATHS are derived.
# Validates that the derived go.mod list matches what exists in the repo.
# If a module is added or removed, this script will fail until updated.
#
# Usage: source this file after setting REPO_ROOT
#   REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
#   source "$REPO_ROOT/scripts/lib_go_modules.sh"

if [[ -z "${REPO_ROOT:-}" ]]; then
    echo "Error: REPO_ROOT must be set before sourcing lib_go_modules.sh" >&2
    exit 1
fi

cd "$REPO_ROOT" || exit

# shellcheck source=scripts/vcs.sh
source "$REPO_ROOT/scripts/vcs.sh"

ROOT_MODULE="github.com/ava-labs/avalanchego"

# Canonical list of release module prefixes. Update when adding or removing modules.
TAG_PREFIXES=(
    ""
    "graft/coreth/"
    "graft/evm/"
    "graft/subnet-evm/"
)

# Derive go.mod paths and module paths from prefixes
GO_MODS=()
MODULE_PATHS=()
for prefix in "${TAG_PREFIXES[@]}"; do
    GO_MODS+=("${prefix}go.mod")
    if [[ -z "$prefix" ]]; then
        MODULE_PATHS+=("$ROOT_MODULE")
    else
        MODULE_PATHS+=("$ROOT_MODULE/${prefix%/}")
    fi
done

# Validate that the derived list matches actual go.mod files in the repo.
# Excludes utility modules (tools/) that are not part of the release.
mapfile -t actual < <(vcs_ls_files 'go.mod' '**/go.mod' | grep -v '^tools/' | sort)
expected="$(printf '%s\n' "${GO_MODS[@]}" | sort)"
actual_str="$(printf '%s\n' "${actual[@]}")"

if [[ "$expected" != "$actual_str" ]]; then
    echo "Error: TAG_PREFIXES in scripts/lib_go_modules.sh is out of date." >&2
    echo "" >&2
    echo "Expected go.mod files:" >&2
    echo "$expected" >&2
    echo "" >&2
    echo "Actual go.mod files:" >&2
    echo "$actual_str" >&2
    echo "" >&2
    echo "Update TAG_PREFIXES in scripts/lib_go_modules.sh" >&2
    exit 1
fi
