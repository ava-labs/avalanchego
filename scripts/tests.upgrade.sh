#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/tests.upgrade.sh 1.7.16
# AOXCGO_PATH=./path/to/aoxc ./scripts/tests.upgrade.sh 1.7.16 # Customization of aoxc path
if ! [[ "$0" =~ scripts/tests.upgrade.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  echo "Missing version argument!"
  echo "Usage: ${0} [VERSION]" >>/dev/stderr
  exit 255
fi

AOXCGO_PATH="$(realpath ${AOXCGO_PATH:-./build/aoxc})"

#################################
# download aoxc
# https://github.com/aoxcs/aoxc/releases
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
DOWNLOAD_URL=https://github.com/aoxcs/aoxc/releases/download/v${VERSION}/aoxc-linux-${GOARCH}-v${VERSION}.tar.gz
DOWNLOAD_PATH=/tmp/aoxc.tar.gz
if [[ ${GOOS} == "darwin" ]]; then
  DOWNLOAD_URL=https://github.com/aoxcs/aoxc/releases/download/v${VERSION}/aoxc-macos-v${VERSION}.zip
  DOWNLOAD_PATH=/tmp/aoxc.zip
fi

rm -f ${DOWNLOAD_PATH}
rm -rf /tmp/aoxc-v${VERSION}
rm -rf /tmp/aoxc-build

echo "downloading aoxc ${VERSION} at ${DOWNLOAD_URL}"
curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

echo "extracting downloaded aoxc"
if [[ ${GOOS} == "linux" ]]; then
  tar xzvf ${DOWNLOAD_PATH} -C /tmp
elif [[ ${GOOS} == "darwin" ]]; then
  unzip ${DOWNLOAD_PATH} -d /tmp/aoxc-build
  mv /tmp/aoxc-build/build /tmp/aoxc-v${VERSION}
fi
find /tmp/aoxc-v${VERSION}

# Sourcing constants.sh ensures that the necessary CGO flags are set to
# build the portable version of BLST. Without this, ginkgo may fail to
# build the test binary if run on a host (e.g. github worker) that lacks
# the instructions to build non-portable BLST.
source ./scripts/constants.sh

#################################
echo "building upgrade.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/upgrade
./tests/upgrade/upgrade.test --help

#################################
# By default, it runs all upgrade test cases!
echo "running upgrade tests against the local cluster with ${AOXCGO_PATH}"
./tests/upgrade/upgrade.test \
  --ginkgo.v \
  --aoxc-path=/tmp/aoxc-v${VERSION}/aoxc \
  --aoxc-path-to-upgrade-to=${AOXCGO_PATH}
