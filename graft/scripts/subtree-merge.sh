#!/usr/bin/env bash

set -euo pipefail

# Performs a git subtree merge of an external repository into a graft subdirectory and then commits the result.
#
# Usage: subtree-merge.sh <module-path> [version]
# Example: subtree-merge.sh github.com/ava-labs/coreth
# Example: subtree-merge.sh github.com/ava-labs/coreth 1a498175
# Example: subtree-merge.sh github.com/ava-labs/coreth master evm
#
# Arguments:
#   module-path: The Go module path (e.g., github.com/ava-labs/coreth)
#   version: (Optional) The version/tag/SHA to merge (can be a tag, branch, or commit SHA)
#            If not provided, the version will be discovered from go.mod
#   target-path: (Optional) The target path within the repository to merge into, relative to the repo root.
#                If not provided, defaults to graft/[repo-name]
#                Cannot be provided without providing version.
#
# The repository URL is constructed by prepending https:// to the module path.
# The target path is automatically derived as graft/[repo-name] where repo-name
# is the last component of the module path, unless provided.
#
# What this script does:
#   1. Constructs repository URL from module path
#   2. Discovers version from go.mod if not provided
#   3. Derives target path from module path (graft/[last-component])
#   4. Validates that target path doesn't already exist
#   5. Adds the external repo as a temporary git remote
#   6. Performs a merge with 'ours' strategy (keeps our history, adds theirs)
#   7. Reads the external repo's tree into the target subdirectory
#   8. Commits the merge with a descriptive message
#   9. Removes the temporary remote

if [ $# -lt 1 ] || [ $# -gt 3 ]; then
  echo "Error:  one to three arguments required" >&2
  echo "Usage: $0 <module-path> [version] [target-path]" >&2
  echo "Example: $0 github.com/ava-labs/coreth" >&2
  echo "Example: $0 github.com/ava-labs/coreth 1a498175" >&2
  echo "Example: $0 github.com/ava-labs/coreth master evm" >&2
  exit 1
fi

MODULE_PATH="$1"

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )
cd "${REPO_ROOT}"

# Discover version from go.mod if not provided
if [ $# -eq 2 ]; then
  VERSION="$2"
  echo "using provided version: ${VERSION}"
else
  echo "discovering version from go.mod"
  VERSION="$(bash "${REPO_ROOT}/graft/scripts/get-module-version.sh" "${MODULE_PATH}")"
  echo "discovered version: ${VERSION}"
fi

# Construct repository URL from module path
REPO_URL="https://${MODULE_PATH}"

# Use graft/[repo-name] as target path, unless one is provided
if [ $# -eq 3 ]; then
  GRAFT_PATH="$3"
else
  # Extract repository name from module path
  # Example: github.com/ava-labs/coreth -> coreth
  REPO_BASENAME="$(basename "${MODULE_PATH}")"
  TARGET_PATH="graft/${REPO_BASENAME}" 
fi

# Check if target path already exists
if [ -d "${REPO_ROOT}/${TARGET_PATH}" ]; then
  echo "Target path ${TARGET_PATH} already exists, skipping subtree merge"
  exit 0
fi

REMOTE_NAME="${REPO_BASENAME}"

echo "adding remote ${REMOTE_NAME} from ${REPO_URL}"
git remote add -f "${REMOTE_NAME}" "${REPO_URL}"

echo "performing subtree merge of ${VERSION} into ${TARGET_PATH}"
git subtree add --prefix="${TARGET_PATH}" "${REMOTE_NAME}" "${VERSION}"

echo "removing ${REMOTE_NAME} remote"
git remote remove "${REMOTE_NAME}"

echo "subtree merge of ${REPO_BASENAME} completed successfully"
