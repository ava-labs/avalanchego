#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

extra_imports=$(grep -r --include='*.go' '"github.com/ethereum/go-ethereum/.*"' -o -h | sort -u | comm -23 - ./scripts/geth-allowed-packages.txt)
if [ ! -z "${extra_imports}" ]; then
    echo "new go-ethereum imports should be added to ./scripts/geth-allow-list.txt to prevent accidental imports:"
    echo "${extra_imports}"
    exit 1
fi

extra_imports=$(grep -r --include='*.go' '"github.com/ava-labs/coreth/.*"' -o -h || true | sort -u)
if [ ! -z "${extra_imports}" ]; then
    echo "subnet-evm should not import packages from coreth:"
    echo "${extra_imports}"
    exit 1
fi
