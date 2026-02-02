#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"

# GOWORK=off is required because -modfile is incompatible with workspace mode,
# and even -C doesn't help since Go walks up the tree to find go.work.
GOWORK=off go tool -modfile="${AVALANCHE_PATH}"/tools/external/go.mod "${@}"
