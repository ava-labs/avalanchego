#!/usr/bin/env bash

set -euo pipefail

# Performs a git subtree merge of a local repository into a graft subdirectory.
#
# Usage: subtree-merge.sh <version> <source-path> <target-path>
# Example: subtree-merge.sh main ../firewood firewood
#
# Arguments:
#   version:     The version/tag/SHA to merge (can be a tag, branch, or commit SHA)
#   source-path: Path to a local git repository, relative to this repository's
#                root when not absolute.
#   target-path: The target path within this repository, relative to its root.
#
# The script refuses to overwrite an existing target path. Roll back the prior
# subtree merge intentionally before retrying.

if [ $# -ne 3 ]; then
  echo "Error: three arguments required" >&2
  echo "Usage: $0 <version> <source-path> <target-path>" >&2
  echo "Example: $0 main ../firewood firewood" >&2
  exit 1
fi

VERSION="$1"
SOURCE_PATH="$2"
TARGET_PATH="$3"
REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

if [ -d "${REPO_ROOT}/${TARGET_PATH}" ]; then
  echo "Error: target path ${TARGET_PATH} already exists." >&2
  echo "Refusing to skip or overwrite an existing graft." >&2

  MERGE_SUMMARY=$(git -C "${REPO_ROOT}" log -1 --format='%h %s' --merges -- "${TARGET_PATH}")
  if [ -n "${MERGE_SUMMARY}" ]; then
    echo "The most recent merge that changed ${TARGET_PATH} is:" >&2
    echo "  ${MERGE_SUMMARY}" >&2
  fi

  echo "Roll back the previous subtree merge intentionally, then retry." >&2
  exit 1
fi

if [ -z "${SOURCE_PATH}" ]; then
  echo "Error: source path is required." >&2
  exit 1
fi

if [[ "${SOURCE_PATH}" != /* ]]; then
  SOURCE_PATH="${REPO_ROOT}/${SOURCE_PATH}"
fi

if [ ! -d "${SOURCE_PATH}" ]; then
  echo "Error: source path ${SOURCE_PATH} does not exist." >&2
  exit 1
fi

SOURCE_PATH=$(cd "${SOURCE_PATH}" && pwd)
REPO_BASENAME="$(basename "${SOURCE_PATH}")"

if ! SOURCE_REPO_ROOT=$(git -C "${SOURCE_PATH}" rev-parse --show-toplevel 2>/dev/null); then
  echo "Error: source path ${SOURCE_PATH} is not a git repository." >&2
  exit 1
fi

if [ "${SOURCE_PATH}" != "${SOURCE_REPO_ROOT}" ]; then
  echo "Error: source path ${SOURCE_PATH} is not the root of a git repository." >&2
  exit 1
fi

cd "${REPO_ROOT}"

TEMP_REMOTE_NAME="subtree-${REPO_BASENAME}-$$"

cleanup() {
  echo "removing ${TEMP_REMOTE_NAME} remote"
  git remote remove "${TEMP_REMOTE_NAME}"
}
trap cleanup EXIT

echo "using provided version: ${VERSION}"
echo "adding temporary remote ${TEMP_REMOTE_NAME} from ${SOURCE_PATH}"
git remote add "${TEMP_REMOTE_NAME}" "${SOURCE_PATH}"
echo "fetching ${VERSION} from ${SOURCE_PATH}"
git fetch "${TEMP_REMOTE_NAME}" "${VERSION}"

echo "performing subtree merge of ${VERSION} into ${TARGET_PATH}"
git subtree add --prefix="${TARGET_PATH}" "${TEMP_REMOTE_NAME}" "${VERSION}"

echo "subtree merge of ${REPO_BASENAME} completed successfully"
