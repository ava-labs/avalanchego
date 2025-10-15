#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH=$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
cd "${AVALANCHE_PATH}" && go tool ginkgo "${@}"
