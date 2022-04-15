#!/usr/bin/env bash
set -e

# e.g.,
# ./scripts/build.sh
# ./scripts/tests.upgrade.sh 1.7.4 ./build/caminogo
if ! [[ "$0" =~ scripts/tests.upgrade.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

VERSION=$1
if [[ -z "${VERSION}" ]]; then
  echo "Missing version argument!"
  echo "Usage: ${0} [VERSION] [NEW-BINARY]" >> /dev/stderr
  exit 255
fi

NEW_BINARY=$2
if [[ -z "${NEW_BINARY}" ]]; then
  echo "Missing new binary path argument!"
  echo "Usage: ${0} [VERSION] [NEW-BINARY]" >> /dev/stderr
  exit 255
fi

#################################
# download caminogo
# https://github.com/chain4travel/caminogo/releases
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
DOWNLOAD_URL=https://github.com/chain4travel/caminogo/releases/download/v${VERSION}/caminogo-linux-${GOARCH}-v${VERSION}.tar.gz
DOWNLOAD_PATH=/tmp/caminogo.tar.gz
if [[ ${GOOS} == "darwin" ]]; then
  DOWNLOAD_URL=https://github.com/chain4travel/caminogo/releases/download/v${VERSION}/caminogo-macos-v${VERSION}.zip
  DOWNLOAD_PATH=/tmp/caminogo.zip
fi

rm -f ${DOWNLOAD_PATH}
rm -rf /tmp/caminogo-v${VERSION}
rm -rf /tmp/caminogo-build

echo "downloading caminogo ${VERSION} at ${DOWNLOAD_URL}"
curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

echo "extracting downloaded caminogo"
if [[ ${GOOS} == "linux" ]]; then
  tar xzvf ${DOWNLOAD_PATH} -C /tmp
elif [[ ${GOOS} == "darwin" ]]; then
  unzip ${DOWNLOAD_PATH} -d /tmp/caminogo-build
  mv /tmp/caminogo-build/build /tmp/caminogo-v${VERSION}
fi
find /tmp/caminogo-v${VERSION}

#################################
# download camino-network-runner
# https://github.com/chain4travel/camino-network-runner
NETWORK_RUNNER_VERSION=0.0.1
DOWNLOAD_PATH=/tmp/camino-network-runner.tar.gz
DOWNLOAD_URL=https://github.com/chain4travel/camino-network-runner/releases/download/v${NETWORK_RUNNER_VERSION}/camino-network-runner_${NETWORK_RUNNER_VERSION}_linux_amd64.tar.gz
if [[ ${GOOS} == "darwin" ]]; then
  DOWNLOAD_URL=https://github.com/chain4travel/camino-network-runner/releases/download/v${NETWORK_RUNNER_VERSION}/camino-network-runner_${NETWORK_RUNNER_VERSION}_darwin_amd64.tar.gz
fi

rm -f ${DOWNLOAD_PATH}
rm -f /tmp/camino-network-runner

echo "downloading camino-network-runner ${NETWORK_RUNNER_VERSION} at ${DOWNLOAD_URL}"
curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

echo "extracting downloaded camino-network-runner"
tar xzvf ${DOWNLOAD_PATH} -C /tmp
/tmp/camino-network-runner -h

#################################
echo "building upgrade.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.0.0
ACK_GINKGO_RC=true ginkgo build ./tests/upgrade
./tests/upgrade/upgrade.test --help

#################################
# run "camino-network-runner" server
echo "launch camino-network-runner in the background"
/tmp/camino-network-runner \
server \
--log-level debug \
--port=":12340" \
--grpc-gateway-port=":12341" 2> /dev/null &
PID=${!}

#################################
# By default, it runs all upgrade test cases!
echo "running upgrade tests against the local cluster with ${NEW_BINARY}"
./tests/upgrade/upgrade.test \
--ginkgo.v \
--log-level debug \
--network-runner-grpc-endpoint="0.0.0.0:12340" \
--caminogo-path=/tmp/caminogo-v${VERSION}/caminogo \
--caminogo-path-to-upgrade=${NEW_BINARY} || EXIT_CODE=$?

kill ${PID}

if [[ ${EXIT_CODE} -gt 0 ]]; then
  echo "FAILURE with exit code ${EXIT_CODE}"
  exit ${EXIT_CODE}
else
  echo "ALL SUCCESS!"
fi
