#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Ensure that there are no eth imports that are not marked as explicitly allowed via ./scripts/eth-allowed-packages.txt
# 1. Recursively search through all go files for any lines that include a direct import from go-ethereum
# 2. Ignore lines that import libevm with a named import starting with "eth"
# 3. Sort the unique results
# 4. Print out the difference between the search results and the list of specified allowed package imports from libevm.
libevm_regexp='"github.com/ava-labs/libevm/.*"'
allow_named_imports='eth\w\+ "'
extra_imports=$(find . -type f \( -name "*.go" \) ! -path "./core/main_test.go" ! -name "gen_*.go" \
  -exec sh -c 'grep "$0" -h "$2" | grep -v "$1" | grep -o "$0"' "${libevm_regexp}" "${allow_named_imports}" {} \; | \
  sort -u | comm -23 - ./scripts/eth-allowed-packages.txt)
if [ -n "${extra_imports}" ]; then
    echo "new ethereum imports should be added to ./scripts/eth-allowed-packages.txt to prevent accidental imports:"
    echo "${extra_imports}"
    exit 1
fi
