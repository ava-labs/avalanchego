#!/usr/bin/env bash

set -euo pipefail

# This script checks that the Go version is consistent across all locations
# where it is defined. See go.mod for the canonical list of locations.

if ! [[ "$0" =~ scripts/check_go_version.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

errors=0

# Extract Go version from various sources
extract_go_mod_version() {
  local file="$1"
  grep -E '^go [0-9]+\.[0-9]+' "$file" | head -1 | awk '{print $2}'
}

extract_nix_version() {
  grep 'goVersion = ' nix/go/default.nix | sed 's/.*"\([^"]*\)".*/\1/'
}

extract_module_bazel_version() {
  grep 'go_sdk.download' MODULE.bazel | sed 's/.*version = "\([^"]*\)".*/\1/'
}

# Get the canonical version from root go.mod
canonical_version=$(extract_go_mod_version go.mod)

if [[ -z "$canonical_version" ]]; then
  echo -e "${RED}ERROR: Could not extract Go version from go.mod${NC}"
  exit 1
fi

echo "Canonical Go version (from go.mod): $canonical_version"
echo ""

check_version() {
  local source="$1"
  local version="$2"

  if [[ -z "$version" ]]; then
    echo -e "${RED}FAIL${NC}: $source - could not extract version"
    errors=$((errors + 1))
  elif [[ "$version" == "$canonical_version" ]]; then
    echo -e "${GREEN}OK${NC}:   $source ($version)"
  else
    echo -e "${RED}FAIL${NC}: $source ($version != $canonical_version)"
    errors=$((errors + 1))
  fi
}

# Check all go.mod files
go_mod_files=(
  "go.mod"
  "tools/go.mod"
  "graft/evm/go.mod"
  "graft/coreth/go.mod"
  "graft/subnet-evm/go.mod"
)

echo "Checking go.mod files..."
for file in "${go_mod_files[@]}"; do
  if [[ -f "$file" ]]; then
    version=$(extract_go_mod_version "$file")
    check_version "$file" "$version"
  else
    echo -e "${RED}FAIL${NC}: $file - file not found"
    errors=$((errors + 1))
  fi
done

echo ""
echo "Checking nix/go/default.nix..."
nix_version=$(extract_nix_version)
check_version "nix/go/default.nix" "$nix_version"

echo ""
echo "Checking MODULE.bazel..."
bazel_version=$(extract_module_bazel_version)
check_version "MODULE.bazel" "$bazel_version"

echo ""
if [[ $errors -gt 0 ]]; then
  echo -e "${RED}$errors version mismatch(es) found!${NC}"
  echo ""
  echo "Please update all locations to use Go $canonical_version."
  echo "See go.mod for the full list of places that need updating."
  exit 1
else
  echo -e "${GREEN}All Go versions are consistent!${NC}"
fi
