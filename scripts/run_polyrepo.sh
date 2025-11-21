#!/usr/bin/env bash

set -euo pipefail

POLYREPO_REVISION=0c4c6fcc92
echo "Running polyrepo@${POLYREPO_REVISION} via go run..."
go run github.com/ava-labs/avalanchego/tests/fixture/polyrepo@"${POLYREPO_REVISION}" "${@}"
