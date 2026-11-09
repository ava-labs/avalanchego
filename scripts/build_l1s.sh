#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_l1s.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/lib_build_plugin.sh

VM_ID=peeeXkQn5W9WwmFVjEx1EEmGcyRPj15XoMH8xNJrdWxmMWSfj  # constants.L1sID
build_plugin "${PLUGIN_BUILD_DIR}/l1s" "${VM_ID}" ./vms/evm/imp/l1s/plugin/
