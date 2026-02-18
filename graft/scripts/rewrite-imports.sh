#!/usr/bin/env bash

set -euo pipefail

# Rewrites Go module imports from an external package to an internal graft subdirectory
# This script modifies the working tree and commits the changes.
#
# Usage: rewrite-imports.sh <original-module-import-path>
# Example: rewrite-imports.sh github.com/ava-labs/coreth
#   Will rewrite: github.com/ava-labs/coreth -> github.com/ava-labs/avalanchego/graft/coreth
# Example: rewrite-imports.sh github.com/ava-labs/coreth github.com/ava-labs/avalanchego/coreth
#   Will rewrite: github.com/ava-labs/coreth -> github.com/ava-labs/avalanchego/coreth
#

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "Error: one or two arguments required"
  echo "Usage: $0 <original-module-import-path> [new-module-import-path]"
  echo "Example: $0 github.com/ava-labs/coreth"
  echo "Example: $0 github.com/ava-labs/coreth github.com/ava-labs/avalanchego/coreth"
  exit 1
fi

ORIGINAL_IMPORT="$1"

# Extract the last component of the import path
# e.g., github.com/ava-labs/coreth -> coreth
PACKAGE_NAME="${ORIGINAL_IMPORT##*/}"

# Determine target import path
if [ $# -eq 2 ]; then
  TARGET_IMPORT="$2"
else
  TARGET_IMPORT="github.com/ava-labs/avalanchego/graft/${PACKAGE_NAME}"
fi

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )
cd "${REPO_ROOT}"

echo "rewriting ${ORIGINAL_IMPORT} imports to ${TARGET_IMPORT} in all golang files"

# Escape dots in the original import path for regex use
ESCAPED_ORIGINAL="${ORIGINAL_IMPORT//./\\.}"

rg "${ESCAPED_ORIGINAL}" -t go --files-with-matches --null | \
  xargs -0 perl -i -pe "s|\\Q${ORIGINAL_IMPORT}\\E|${TARGET_IMPORT}|g"

echo "import rewrite completed successfully"

echo "staging changes..."
git add -u '*.go' # vcs-ok: graft workflow uses git porcelain for staging/committing

echo "committing changes..."
# vcs-ok: graft workflow uses git porcelain for staging/committing
git commit -S -m "Rewrite ${ORIGINAL_IMPORT} imports to ${TARGET_IMPORT}

Rewrites all Go import statements from external package ${ORIGINAL_IMPORT}
to internal graft subdirectory ${TARGET_IMPORT}."

echo "changes committed successfully"
