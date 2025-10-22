#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
cd "${REPO_ROOT}"

CORETH_REPO=https://github.com/ava-labs/coreth.git
CORETH_REMOTE="coreth"
CORETH_BRANCH="${CORETH_REMOTE}/master"
GRAFT_PATH=graft/coreth/

echo "Adding remote ${CORETH_REMOTE} to avalanchego clone"
git remote add -f "${CORETH_REMOTE}" "${CORETH_REPO}"

echo "Preparing a merge of ${CORETH_BRANCH} using 'ours' strategy"
git merge -s ours --no-commit --allow-unrelated-histories "${CORETH_BRANCH}"

echo "Reading the coreth tree into the ${GRAFT_PATH} path"
git read-tree --prefix="${GRAFT_PATH}" -u "${CORETH_BRANCH}"

git commit -m "Merge coreth repository as subdirectory at ${GRAFT_PATH}"
git remote remove coreth
