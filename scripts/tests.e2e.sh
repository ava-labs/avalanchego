#!/usr/bin/env bash
set -e

# e.g.,
# ./scripts/build.sh
# ./scripts/tests.e2e.sh ./build/caminogo
# ENABLE_WHITELIST_VTX_TESTS=true ./scripts/tests.e2e.sh ./build/caminogo
if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

CAMINOGO_PATH=$1
if [[ -z "${CAMINOGO_PATH}" ]]; then
  echo "Missing CAMINOGO_PATH argument!"
  echo "Usage: ${0} [CAMINOGO_PATH]" >> /dev/stderr
  exit 255
fi

ENABLE_WHITELIST_VTX_TESTS=${ENABLE_WHITELIST_VTX_TESTS:-false}

#################################
# download camino-network-runner
# https://github.com/chain4travel/camino-network-runner
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
NETWORK_RUNNER_VERSION=0.0.2
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
echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.0.0
ACK_GINKGO_RC=true ginkgo build ./tests/e2e
./tests/e2e/e2e.test --help

#################################
# run "camino-network-runner" server
echo "launch camino-network-runner in the background"
/tmp/camino-network-runner \
server \
--log-level debug \
--port=":12342" \
--grpc-gateway-port=":12343" 2> /dev/null &
PID=${!}

#################################
# By default, it runs all e2e test cases!
# Use "--ginkgo.skip" to skip tests.
# Use "--ginkgo.focus" to select tests.
#
# to run only ping tests:
# --ginkgo.focus "\[Local\] \[Ping\]"
#
# to run only X-Chain whitelist vtx tests:
# --ginkgo.focus "\[X-Chain\] \[WhitelistVtx\]"
#
# to skip all "Local" tests
# --ginkgo.skip "\[Local\]"
#
# set "--enable-whitelist-vtx-tests" to explicitly enable/disable whitelist vtx tests
echo "running e2e tests against the local cluster with ${CAMINOGO_PATH}"
./tests/e2e/e2e.test \
--ginkgo.v \
--ginkgo.skip "\[Local\]" \
--log-level debug \
--network-runner-grpc-endpoint="0.0.0.0:12342" \
--caminogo-log-level=INFO \
--caminogo-path=${CAMINOGO_PATH} \
--enable-whitelist-vtx-tests=${ENABLE_WHITELIST_VTX_TESTS} || EXIT_CODE=$?

kill ${PID}

if [[ ${EXIT_CODE} -gt 0 ]]; then
  echo "FAILURE with exit code ${EXIT_CODE}"
  exit ${EXIT_CODE}
else
  echo "ALL SUCCESS!"
fi
