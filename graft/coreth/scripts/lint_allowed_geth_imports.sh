#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Ensure that there are no geth imports that are not marked as explicitly allowed via ./scripts/geth-allowed-packages.txt
# 1. Recursively search through all go files for any lines that include a direct import from go-ethereum
# 2. Sort the unique results
# #. Print out the difference between the search results and the list of specified allowed package imports from geth.
extra_imports=$(grep -r --include='*.go' '"github.com/ethereum/go-ethereum/.*"' -o -h | sort -u | comm -23 - ./scripts/geth-allowed-packages.txt)
if [ ! -z "${extra_imports}" ]; then
    echo "new go-ethereum imports should be added to ./scripts/geth-allowed-packages.txt to prevent accidental imports:"
    echo "${extra_imports}"
    exit 1
fi
