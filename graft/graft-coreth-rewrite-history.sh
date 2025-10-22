#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
cd "${REPO_ROOT}"

CORETH_REPO=https://github.com/ava-labs/coreth.git
CORETH_REMOTE="coreth-rewritten"
CLONE_PATH="${REPO_ROOT}/tmp/${CORETH_REMOTE}"
GRAFT_PATH=/graft/coreth

if [ -d tmp/coreth-rewritten ]; then
  echo "error: target path ${CLONE_PATH} already exists, remove it before running this script"
  exit 1
fi

echo "Cloning coreth repository to ${CLONE_PATH}"
mkdir -p tmp
git clone "${CORETH_REPO}" "${CLONE_PATH}"
cd "${CLONE_PATH}"

echo "Filtering coreth repo to be rooted at graft/coreth in preparation for grafting to avalanchego"
git filter-repo --to-subdirectory-filter graft/coreth --force

cd "${REPO_ROOT}"

if ! git remote | grep -q "^${CORETH_REMOTE}$"; then
  echo "Adding coreth remote ${CORETH_REMOTE} to avalanchego clone"
  git remote add "${CORETH_REMOTE}" "${CLONE_PATH}"
fi

echo "Grafting coreth into avalanchego"
git fetch "${CORETH_REMOTE}"
git merge --allow-unrelated-histories "${CORETH_REMOTE}/master" -m "Merge coreth repository at ${GRAFT_PATH}"
git remote remove "${CORETH_REMOTE}"
