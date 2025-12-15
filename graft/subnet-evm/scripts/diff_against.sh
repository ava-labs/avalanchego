#!/bin/bash

#
# Usage: scripts/diff_against.sh {geth_commit}
#
# Per-file diffs will be written to `diffs/{geth_commit}`.
#
# Example: `scripts/diff_against.sh b20b4a71598481443d60b261d3e5dcb37f8a0d82` to compare with v1.13.8.
# 
# *NOTE* Before running this script:
# 1. Run `scripts/format_as_upstream.sh` to reformat this repo as `ethereum/go-ethereum`.
# 2. Add geth as a remote: `git remote add -f geth git@github.com:ethereum/go-ethereum.git`.
# 3. Find the geth commit for the latest version merged into this repo.
#

set -e;
set -u;

ROOT=$(git rev-parse --show-toplevel);
cd "${ROOT}";

BASE="${1}";

# The diffs/ directory is in .gitignore to avoid recursive diffiffiffs.
mkdir -p "diffs/${BASE}";

git diff "${BASE}" --name-only | while read -r f
do
    echo "${f}";
    DIR=$(dirname "${f}");
    mkdir -p "diffs/${BASE}/${DIR}";
    git diff "${BASE}" -- "${f}" > "diffs/${BASE}/${f}.diff";
done
