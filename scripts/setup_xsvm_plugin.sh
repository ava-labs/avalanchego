#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/setup_xsvm_plugin.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

for plugin_dir in "${HOME}/.avalanchego/plugins" "${PWD}/build/plugins"; do
  mkdir -p "${plugin_dir}"
  ln -sf "${PWD}/build/xsvm" "${plugin_dir}/v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH"
done
