#!/usr/bin/env bash

set -euo pipefail

# Checks that go version directives are consistent across all go.mod, go.work,
# MODULE.bazel (Bazel Go SDK), and nix/go/default.nix.

if ! [[ "$0" =~ scripts/check_go_version.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# shellcheck source=scripts/vcs.sh
source "$(dirname "${BASH_SOURCE[0]}")/vcs.sh"

# Reference version from go.work
reference=$(go work edit -json go.work | jq -r .Go)

mismatches=()

# Check all go.mod files
while IFS= read -r -d '' mod_file; do
  version=$(go mod edit -json "$mod_file" | jq -r .Go)
  if [[ "$version" != "$reference" ]]; then
    mismatches+=("$mod_file: $version")
  fi
done < <(vcs_ls_files -z 'go.mod' '*/go.mod')

# Check MODULE.bazel version (Bazel Go SDK)
bazel_file="MODULE.bazel"
bazel_version=$(sed -n 's/^go_sdk\.download(version = "\(.*\)")$/\1/p' "$bazel_file")
if [[ -z "$bazel_version" ]]; then
  echo "error: failed to parse go_sdk.download version from $bazel_file"
  exit 1
elif [[ "$bazel_version" != "$reference" ]]; then
  mismatches+=("$bazel_file: $bazel_version")
fi

# Check nix version
nix_file="nix/go/default.nix"
nix_version=$(sed -n 's/^[[:space:]]*goVersion = "\(.*\)";$/\1/p' "$nix_file")
if [[ -z "$nix_version" ]]; then
  echo "error: failed to parse goVersion from $nix_file"
  exit 1
elif [[ "$nix_version" != "$reference" ]]; then
  mismatches+=("$nix_file: $nix_version")
fi

if [[ ${#mismatches[@]} -gt 0 ]]; then
  echo "go version mismatch (expected $reference from go.work):"
  for m in "${mismatches[@]}"; do
    echo "  $m"
  done
  exit 1
fi

echo "All go version directives are consistent: $reference"
