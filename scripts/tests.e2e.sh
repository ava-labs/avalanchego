#!/usr/bin/env bash
set -e
set -o nounset
set -o pipefail

# e.g.,
# ./scripts/build.sh
# ./scripts/tests.e2e.sh ./build/caminogo
# ENABLE_WHITELIST_VTX_TESTS=false ./scripts/tests.e2e.sh ./build/caminogo ./tools/camino-network-runner/bin/camino-network-runner
if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

echo "Run caminogo e2e tests..."

CAMINOGO_PATH="${1-}"
if [[ -z "${CAMINOGO_PATH}" ]]; then
  echo "Missing CAMINOGO_PATH argument!"
  echo "Usage: ${0} [CAMINOGO_PATH]" >> /dev/stderr
  exit 255
fi

CAMINO_NETWORK_RUNNER_PATH="${2-}"
if [[ -z "${CAMINO_NETWORK_RUNNER_PATH}" ]]; then
  echo "Missing CAMINO_NETWORK_RUNNER_PATH argument!"
  echo "Usage: ${0} [CAMINOGO_PATH] [CAMINO_NETWORK_RUNNER_PATH]" >> /dev/stderr
  exit 255
fi

ENABLE_WHITELIST_VTX_TESTS=${ENABLE_WHITELIST_VTX_TESTS:-false}
# ref. https://onsi.github.io/ginkgo/#spec-labels
GINKGO_LABEL_FILTER="!whitelist-tx"
if [[ ${ENABLE_WHITELIST_VTX_TESTS} == true ]]; then
  # run only "whitelist-tx" tests, no other test
  GINKGO_LABEL_FILTER="whitelist-tx"
fi
echo GINKGO_LABEL_FILTER: ${GINKGO_LABEL_FILTER}

cp $CAMINO_NETWORK_RUNNER_PATH /tmp/camino-network-runner

GOPATH="$(go env GOPATH)"
PATH="${GOPATH}/bin:${PATH}"

#################################
echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/e2e
#./tests/e2e/e2e.test --help

#################################
# run "camino-network-runner" server
echo "launch camino-network-runner in the background"
/tmp/camino-network-runner \
server \
--log-level debug \
--port=":12342" \
--disable-grpc-gateway 2> /dev/null &
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
--log-level debug \
--network-runner-grpc-endpoint="0.0.0.0:12342" \
--network-runner-camino-node-path=${CAMINOGO_PATH} \
--network-runner-camino-log-level="WARN" \
--test-keys-file=tests/test.insecure.secp256k1.keys --ginkgo.label-filter="${GINKGO_LABEL_FILTER}" \
&& EXIT_CODE=$? || EXIT_CODE=$?

kill ${PID}

if [[ ${EXIT_CODE} -gt 0 ]]; then
  echo "FAILURE with exit code ${EXIT_CODE}"
  exit ${EXIT_CODE}
else
  echo "ALL SUCCESS!"
fi
