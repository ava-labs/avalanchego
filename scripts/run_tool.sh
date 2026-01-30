#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
go tool -modfile="${AVALANCHE_PATH}"/tools/external/go.mod "${@}"
