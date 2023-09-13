#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/build_timestampvm.sh
if ! [[ "$0" =~ scripts/build_timestampvm.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh

echo "Building timestampvm plugin..."
go build -o ./build/timestampvm ./vms/example/timestampvm/main/

PLUGIN_DIR="$HOME/.avalanchego/plugins"
PLUGIN_PATH="${PLUGIN_DIR}/tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"
echo "Symlinking ./build/timestampvm to ${PLUGIN_PATH}"
mkdir -p "${PLUGIN_DIR}"
ln -sf "${PWD}/build/timestampvm" "${PLUGIN_PATH}"
