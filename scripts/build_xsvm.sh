#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_xsvm.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh

race=''
if [[ "${1:-}" == '-r' ]]; then
  race='-race'
fi

echo "Building xsvm plugin..."
go build ${race} -o ./build/xsvm ./vms/example/xsvm/cmd/xsvm/

# Symlink to both global and local plugin directories to simplify
# usage for testing. The local directory should be preferred but the
# global directory remains supported for backwards compatibility.
./scripts/setup_xsvm_plugin.sh
