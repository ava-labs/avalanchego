#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_xsvm.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh

echo "Building xsvm plugin..."
go build -o ./build/xsvm ./vms/example/xsvm/cmd/xsvm/

PLUGIN_DIR="$HOME/.aoxc/plugins"
PLUGIN_PATH="${PLUGIN_DIR}/2bGh8qfVHsWmYc4t7oXdXv3m4wPxaHpvGrK6PRW3idZrPHyurVH"
echo "Symlinking ./build/xsvm to ${PLUGIN_PATH}"
mkdir -p "${PLUGIN_DIR}"
ln -sf "${PWD}/build/xsvm" "${PLUGIN_PATH}"
