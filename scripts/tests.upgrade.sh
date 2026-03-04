#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/tests.upgrade.sh                                               # Use default version
# ./scripts/tests.upgrade.sh 1.11.0                                        # Specify a version
# AVALANCHEGO_PATH=./path/to/avalanchego ./scripts/tests.upgrade.sh 1.11.0 # Customization of avalanchego path
if ! [[ "$0" =~ scripts/tests.upgrade.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# The AvalancheGo local network does not support long-lived
# backwards-compatible networks. When a breaking change is made to the
# local network, this flag must be updated to the last compatible
# version with the latest code.
#
# v1.14.0 is the earliest version that supports Granite.
DEFAULT_VERSION="1.14.0"

VERSION="${1:-${DEFAULT_VERSION}}"
if [[ -z "${VERSION}" ]]; then
  echo "Missing version argument!"
  echo "Usage: ${0} [VERSION]" >>/dev/stderr
  exit 255
fi

AVALANCHEGO_PATH="$(realpath "${AVALANCHEGO_PATH:-./build/avalanchego}")"

#################################
# download avalanchego
# https://github.com/ava-labs/avalanchego/releases
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
DOWNLOAD_URL=https://github.com/ava-labs/avalanchego/releases/download/v${VERSION}/avalanchego-linux-${GOARCH}-v${VERSION}.tar.gz
DOWNLOAD_PATH=/tmp/avalanchego.tar.gz
if [[ ${GOOS} == "darwin" ]]; then
  DOWNLOAD_URL=https://github.com/ava-labs/avalanchego/releases/download/v${VERSION}/avalanchego-macos-v${VERSION}.zip
  DOWNLOAD_PATH=/tmp/avalanchego.zip
fi

rm -f ${DOWNLOAD_PATH}
rm -rf "/tmp/avalanchego-v${VERSION}"
rm -rf /tmp/avalanchego-build

echo "downloading avalanchego ${VERSION} at ${DOWNLOAD_URL}"
curl -L "${DOWNLOAD_URL}" -o "${DOWNLOAD_PATH}"

echo "extracting downloaded avalanchego"
if [[ ${GOOS} == "linux" ]]; then
  tar xzvf ${DOWNLOAD_PATH} -C /tmp
elif [[ ${GOOS} == "darwin" ]]; then
  unzip ${DOWNLOAD_PATH} -d /tmp/avalanchego-build
  mv /tmp/avalanchego-build/build "/tmp/avalanchego-v${VERSION}"
fi
find "/tmp/avalanchego-v${VERSION}"

# Sourcing constants.sh ensures that the necessary CGO flags are set to
# build the portable version of BLST. Without this, ginkgo may fail to
# build the test binary if run on a host (e.g. github worker) that lacks
# the instructions to build non-portable BLST.
source ./scripts/constants.sh

#################################
# By default, it runs all upgrade test cases!
echo "running upgrade tests against the local cluster with ${AVALANCHEGO_PATH}"
./bin/ginkgo -v ./tests/upgrade -- \
  --avalanchego-path="/tmp/avalanchego-v${VERSION}/avalanchego" \
  --avalanchego-path-to-upgrade-to="${AVALANCHEGO_PATH}"
