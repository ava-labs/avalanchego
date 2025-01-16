#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_xsvm.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh

echo "Building xsvm plugin..."
go build -o ./build/xsvm ./vms/example/xsvm/cmd/xsvm/

# Symlink to both global and local plugin directories to simplify
# usage for testing. The local directory should be preferred but the
# global directory remains supported for backwards compatibility.
LOCAL_PLUGIN_PATH="${PWD}/build/plugins"
GLOBAL_PLUGIN_PATH="${HOME}/.avalanchego/plugins"
for plugin_dir in "${GLOBAL_PLUGIN_PATH}" "${LOCAL_PLUGIN_PATH}"; do
  PLUGIN_PATH="${plugin_dir}/v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH"
  echo "Symlinking ./build/xsvm to ${PLUGIN_PATH}"
  mkdir -p "${plugin_dir}"
  ln -sf "${PWD}/build/xsvm" "${PLUGIN_PATH}"
done
