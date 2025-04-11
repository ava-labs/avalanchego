#!/usr/bin/env bash

set -euo pipefail

# Ensure the go command is run from the root of the repository so that its go.mod file is used
AVALANCHE_PATH=$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
cd "${AVALANCHE_PATH}"

# Installing and then running is faster than `go run`.
GOBIN="${AVALANCHE_PATH}/build" go install github.com/onsi/ginkgo/v2/ginkgo
./build/ginkgo "${@}"
