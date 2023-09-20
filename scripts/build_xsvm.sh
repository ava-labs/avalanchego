#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_xsvm.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh

PLUGIN_FILENAME="v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH"

# Set the PATHS
GOPATH="$(go env GOPATH)"
if [[ $# -eq 1 ]]; then
    BINARY_DIRECTORY=$1
elif [[ $# -eq 0 ]]; then
    BINARY_DIRECTORY="${GOPATH}/src/github.com/ava-labs/avalanchego/build/plugins/${PLUGIN_FILENAME}"
else
    echo "Invalid arguments to build xsvm. Requires either no arguments (default) or one arguments to specify plugin location."
    exit 1
fi

echo "Building xsvm plugin into ${BINARY_DIRECTORY}"
go build -o "${BINARY_DIRECTORY}" ./vms/example/xsvm/cmd/xsvm/

echo "Symlinking ${BINARY_DIRECTORY} to ./build/xsvm"
mkdir -p build
ln -sf "${BINARY_DIRECTORY}" ./build/xsvm
