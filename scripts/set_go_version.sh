#!/usr/bin/env bash

set -euo pipefail

# Updates go version directives across all go.mod and go.work files.
# Does NOT update nix/go/default.nix (requires SHA changes).

if ! [[ "$0" =~ scripts/set_go_version.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# shellcheck source=scripts/vcs.sh
source "$(dirname "${BASH_SOURCE[0]}")/vcs.sh"

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <go-version>"
  echo "Example: $0 1.25.7"
  exit 1
fi

version="$1"

go work edit -go="$version" go.work
echo "updated go.work"

while IFS= read -r -d '' mod_file; do
  go mod edit -go="$version" "$mod_file"
  echo "updated $mod_file"
done < <(vcs_ls_files -z 'go.mod' '*/go.mod')

echo ""
echo "NOTE: nix/go/default.nix requires manual update (version + SHA256 checksums)."
