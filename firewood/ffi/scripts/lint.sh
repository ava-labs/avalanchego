#!/usr/bin/env bash

set -euo pipefail

FFI_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"

cd "${FFI_PATH}"
./scripts/run_tool.sh golangci-lint run --config .golangci.yaml "${@:-./...}"
