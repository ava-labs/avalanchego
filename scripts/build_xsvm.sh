#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_xsvm.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh

echo "Building xsvm plugin..."
go build -o ./build/xsvm ./vms/example/xsvm/cmd/xsvm/

PLUGIN_DIR="$HOME/.avalanchego/plugins"
PLUGIN_PATH="${PLUGIN_DIR}/v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH"
echo "Symlinking ./build/xsvm to ${PLUGIN_PATH}"
mkdir -p "${PLUGIN_DIR}"
ln -sf "${PWD}/build/xsvm" "${PLUGIN_PATH}"
