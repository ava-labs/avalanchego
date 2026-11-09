#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_xsvm.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/lib_build_plugin.sh

VM_ID=v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH  # constants.XSVMID
build_plugin "${PLUGIN_BUILD_DIR}/xsvm" "${VM_ID}" ./vms/example/xsvm/cmd/xsvm/
