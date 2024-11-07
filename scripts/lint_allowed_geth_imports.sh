#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Ensure that there are no geth imports that are not marked as explicitly allowed via ./scripts/geth-allowed-packages.txt
# 1. Recursively search through all go files for any lines that include a direct import from go-ethereum
# 2. Ignore lines that import go-ethereum with a named import starting with "geth"
# 3. Sort the unique results
# 4. Print out the difference between the search results and the list of specified allowed package imports from geth.
geth_regexp='"github.com/ava-labs/libevm/.*"'
allow_named_imports='geth\w\+ "'
extra_imports=$(grep -r --include='*.go' --exclude=mocks.go "${geth_regexp}" -h | grep -v "${allow_named_imports}" | grep -o "${geth_regexp}" | sort -u | comm -23 - ./scripts/geth-allowed-packages.txt)
if [ -n "${extra_imports}" ]; then
    echo "new go-ethereum imports should be added to ./scripts/geth-allowed-packages.txt to prevent accidental imports:"
    echo "${extra_imports}"
    exit 1
fi
