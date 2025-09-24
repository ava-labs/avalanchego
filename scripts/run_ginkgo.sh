#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH=$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
"${AVALANCHE_PATH}"/scripts/run_tool.sh ginkgo "${@}"
