#!/usr/bin/env bash

set -euo pipefail

# Rewrites Go module imports from an external package to an internal graft subdirectory
# This script modifies the working tree only and does NOT commit.
#
# Usage: rewrite-imports.sh <original-module-import-path>
# Example: rewrite-imports.sh github.com/ava-labs/coreth
#   Will rewrite: github.com/ava-labs/coreth -> github.com/ava-labs/avalanchego/graft/coreth

if [ $# -ne 1 ]; then
  echo "Error: exactly one argument required"
  echo "Usage: $0 <original-module-import-path>"
  echo "Example: $0 github.com/ava-labs/coreth"
  exit 1
fi

ORIGINAL_IMPORT="$1"

# Extract the last component of the import path
# e.g., github.com/ava-labs/coreth -> coreth
PACKAGE_NAME="${ORIGINAL_IMPORT##*/}"

TARGET_IMPORT="github.com/ava-labs/avalanchego/graft/${PACKAGE_NAME}"

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )
cd "${REPO_ROOT}"

echo "rewriting ${ORIGINAL_IMPORT} imports to ${TARGET_IMPORT} in all golang files"

# Escape dots in the original import path for regex use
ESCAPED_ORIGINAL=$(echo "${ORIGINAL_IMPORT}" | sed 's/\./\\./g')

rg "${ESCAPED_ORIGINAL}" -t go --files-with-matches --null | \
  xargs -0 perl -i -pe "s|\\Q${ORIGINAL_IMPORT}\\E|${TARGET_IMPORT}|g"

echo "import rewrite completed successfully"
