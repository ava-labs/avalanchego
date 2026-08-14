#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat >&2 <<EOF
Usage: $0 <version> [--dry-run] <source-path> <target-path>

Merge a version from a local Git repository into this repository while
preserving its history. --dry-run validates the source and fetches the version
without creating the subtree merge commit.

Arguments:
  version      Branch, tag, or commit SHA to merge.
  source-path  Root of the local Git repository to merge from. Relative paths
               are resolved from this repository's root.
  target-path  Destination relative to this repository's root. It must not
               contain ".." and must not already exist.

Source paths may be absolute. Roll back a previous subtree merge intentionally
before running this script again.

Examples:
  $0 main ../firewood firewood
  $0 main --dry-run ../firewood firewood
EOF
}

if [ $# -lt 3 ] || [ $# -gt 4 ]; then
  usage
  exit 1
fi

VERSION="$1"
shift
DRY_RUN=false
if [ "${1}" = "--dry-run" ]; then
  DRY_RUN=true
  shift
fi

if [ $# -ne 2 ]; then
  usage
  exit 1
fi

SOURCE_PATH="$1"
TARGET_PATH="$2"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

case "${TARGET_PATH}" in
  "" | /* | . | .. | ../* | */.. | */../*)
    echo "Error: target path must be relative to the repository root and must not contain .." >&2
    exit 1
    ;;
esac

if [ -e "${REPO_ROOT}/${TARGET_PATH}" ] || [ -L "${REPO_ROOT}/${TARGET_PATH}" ]; then
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

if [ "${DRY_RUN}" = false ] && { ! git diff --quiet || ! git diff --cached --quiet; }; then
  echo "Error: working tree has modifications." >&2
  echo "Commit or stash them before performing a subtree merge." >&2
  exit 1
fi

TEMP_REMOTE_NAME="subtree-${REPO_BASENAME}-$$"
TEMP_REMOTE_ADDED=false

cleanup() {
  if [ "${TEMP_REMOTE_ADDED}" = true ]; then
    echo "removing ${TEMP_REMOTE_NAME} remote"
    git remote remove "${TEMP_REMOTE_NAME}"
  fi
}
trap cleanup EXIT

echo "using provided version: ${VERSION}"
echo "adding temporary remote ${TEMP_REMOTE_NAME} from ${SOURCE_PATH}"
git remote add "${TEMP_REMOTE_NAME}" "${SOURCE_PATH}"
TEMP_REMOTE_ADDED=true
echo "fetching ${VERSION} from ${SOURCE_PATH}"
git fetch "${TEMP_REMOTE_NAME}" "${VERSION}"

if [ "${DRY_RUN}" = true ]; then
  echo "dry run completed successfully; no subtree merge was performed"
  exit 0
fi

echo "performing subtree merge of ${VERSION} into ${TARGET_PATH}"
git subtree add --prefix="${TARGET_PATH}" "${TEMP_REMOTE_NAME}" "${VERSION}"

echo "subtree merge of ${REPO_BASENAME} completed successfully"
