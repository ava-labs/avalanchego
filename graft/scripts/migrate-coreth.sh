#!/usr/bin/env bash

set -euo pipefail

# Instructions for use:
#
# - Start with a clone of avalanchego
# - Cherry-pick the commit adding this script from maru-ava:maru/graft-coreth-subtree-merge
#   - Or just copy the script to ./graft/scripts/migrate-coreth.sh
# - Run the migration script from the root of the avalanchego repo: `bash ./graft/scripts/migrate-coreth.sh`
# - Cherry-pick the commits from maru-ava:maru/graft-coreth-subtree-merge after the 'Post-graft mechanical migration' commit

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )
cd "${REPO_ROOT}"

CORETH_REPO=https://github.com/ava-labs/coreth.git
CORETH_REMOTE="coreth"
GRAFT_PATH=graft/coreth

if [ -d "${REPO_ROOT}/${GRAFT_PATH}" ]; then
  echo "coreth already grafted at ${GRAFT_PATH}, skipping grafting process"
else
  echo "getting coreth module details from go.mod to ensure a compatible version is used"
  MODULE_DETAILS="$(go list -m "github.com/ava-labs/coreth" 2>/dev/null)"

  echo "extracting coreth version from ${MODULE_DETAILS}"
  CORETH_VERSION="$(echo "${MODULE_DETAILS}" | awk '{print $2}')"

  echo "checking if ${CORETH_VERSION} is a module hash (e.g. v*YYYYMMDDHHMMSS-abcdef123456)"
  if [[ "${CORETH_VERSION}" =~ ^v.*[0-9]{14}-[0-9a-f]{12}$ ]]; then
    # Extract module hash from version
    MODULE_HASH="$(echo "${CORETH_VERSION}" | grep -Eo '[0-9a-f]{12}$')"
    # The first 8 chars of the hash is used as the tag of avalanchego images
    CORETH_VERSION="${MODULE_HASH::8}"
    echo "using SHA ${CORETH_VERSION} for coreth version"
  else
    # Tags are fetched directly without remote namespace
    echo "using tag ${CORETH_VERSION} for coreth version"
  fi

  echo "adding remote ${CORETH_REMOTE} to avalanchego clone"
  git remote add -f "${CORETH_REMOTE}" "${CORETH_REPO}"

  echo "preparing a merge of ${CORETH_VERSION} using 'ours' strategy"
  git merge -s ours --no-commit --allow-unrelated-histories "${CORETH_VERSION}"

  echo "reading the coreth tree into the ${GRAFT_PATH} path"
  git read-tree --prefix="${GRAFT_PATH}/" -u "${CORETH_VERSION}"

  git commit -m "merge coreth repository as subdirectory at ${GRAFT_PATH} for version ${CORETH_VERSION}"
  git remote remove coreth
fi

echo "rewriting github.com/ava-labs/coreth imports to github.com/ava-labs/avalanchego/graft/coreth in all go files"
rg "github\.com/ava-labs/coreth" -t go --files-with-matches --null | xargs -0 sed -i.bak 's|github\.com/ava-labs/coreth|github.com/ava-labs/avalanchego/graft/coreth|g' && find . -name "*.go.bak" -delete

if [ -f "graft/coreth/go.mod" ]; then
  echo "removing coreth module config files"
  git rm -f graft/coreth/go.*
  go mod tidy
else
  echo "no coreth module config files found to remove"
fi

if [ -f "graft/coreth/.envrc" ]; then
  echo "removing coreth .envrc file"
  git rm -f graft/coreth/.envrc
fi

echo "committing mechanical migration changes"
git add -A
git commit -m "Post-graft mechanical migration

- Rewrite imports from github.com/ava-labs/coreth to github.com/ava-labs/avalanchego/graft/coreth
- Remove coreth module config files
- Remove coreth .envrc file"

echo "coreth migration completed"
