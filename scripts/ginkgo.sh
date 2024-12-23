#!/usr/bin/env bash

set -euo pipefail

# Run the ginkgo version from go.mod
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
${AVALANCHE_PATH}/scripts/av.sh tool ginkgo "$@"
