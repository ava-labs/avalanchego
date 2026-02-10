#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# shellcheck disable=SC1091
source "${AVALANCHE_PATH}/scripts/vcs.sh"

# Show the status of the working tree.
vcs_status_short

# Exits if any uncommitted changes are found.
vcs_is_clean
