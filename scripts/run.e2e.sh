#!/usr/bin/env bash
set -e

# run with
# ./scripts/run.sh 1.7.11
if ! [[ "$0" =~ scripts/run.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

VERSION=$1
if [[ -z "${VERSION}" ]]; then
  echo "Missing version argument!"
  echo "Usage: ${0} [VERSION]" >> /dev/stderr
  exit 255
fi

GENESIS_ADDRESS=0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC
NETWORK_RUNNER_VERSION=1.0.16

AVALANCHE_LOG_LEVEL=${AVALANCHE_LOG_LEVEL:-INFO}

echo "Running with:"
echo VERSION: ${VERSION}
echo GENESIS_ADDRESS: ${GENESIS_ADDRESS}
echo AVALANCHE_LOG_LEVEL: ${AVALANCHE_LOG_LEVEL}

############################
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

rm -rf /tmp/avalanchego-v${VERSION}
rm -f ${DOWNLOAD_PATH}

echo "downloading avalanchego ${VERSION} at ${DOWNLOAD_URL}"
curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

echo "extracting downloaded avalanchego"
if [[ ${GOOS} == "linux" ]]; then
  tar xzvf ${DOWNLOAD_PATH} -C /tmp
elif [[ ${GOOS} == "darwin" ]]; then
  unzip ${DOWNLOAD_PATH} -d /tmp/avalanchego-build
  mv /tmp/avalanchego-build/build /tmp/avalanchego-v${VERSION}
fi
find /tmp/avalanchego-v${VERSION}

AVALANCHEGO_PATH=/tmp/avalanchego-v${VERSION}/avalanchego
AVALANCHEGO_PLUGIN_DIR=/tmp/avalanchego-v${VERSION}/plugins

#################################
# compile subnet-evm
# Check if SUBNET_EVM_COMMIT is set, if not retrieve the last commit from the repo.
# This is used in the Dockerfile to allow a commit hash to be passed in without
# including the .git/ directory within the Docker image.
subnet_evm_commit=${SUBNET_EVM_COMMIT:-$( git rev-list -1 HEAD )}

# Build Subnet EVM, which is run as a subprocess
echo "Building Subnet EVM Version: $subnet_evm_version; GitCommit: $subnet_evm_commit"
go build \
-ldflags "-X github.com/ava-labs/subnet_evm/plugin/evm.GitCommit=$subnet_evm_commit -X github.com/ava-labs/subnet_evm/plugin/evm.Version=$subnet_evm_version" \
-o /tmp/avalanchego-v${VERSION}/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy \
"plugin/"*.go
find /tmp/avalanchego-v${VERSION}

#################################
# download avalanche-network-runner
# https://github.com/ava-labs/avalanche-network-runner
# TODO: use "go install -v github.com/ava-labs/avalanche-network-runner/cmd/avalanche-network-runner@v${NETWORK_RUNNER_VERSION}"
DOWNLOAD_PATH=/tmp/avalanche-network-runner.tar.gz
DOWNLOAD_URL=https://github.com/ava-labs/avalanche-network-runner/releases/download/v${NETWORK_RUNNER_VERSION}/avalanche-network-runner_${NETWORK_RUNNER_VERSION}_linux_amd64.tar.gz
if [[ ${GOOS} == "darwin" ]]; then
  DOWNLOAD_URL=https://github.com/ava-labs/avalanche-network-runner/releases/download/v${NETWORK_RUNNER_VERSION}/avalanche-network-runner_${NETWORK_RUNNER_VERSION}_darwin_amd64.tar.gz
fi

rm -f ${DOWNLOAD_PATH}
rm -f /tmp/avalanche-network-runner

echo "downloading avalanche-network-runner ${NETWORK_RUNNER_VERSION} at ${DOWNLOAD_URL}"
curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

echo "extracting downloaded avalanche-network-runner"
tar xzvf ${DOWNLOAD_PATH} -C /tmp

#################################
echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.3
ACK_GINKGO_RC=true ginkgo build ./tests/e2e

#################################
# run "avalanche-network-runner" server
echo "launch avalanche-network-runner in the background"
/tmp/avalanche-network-runner \
server \
--log-level debug \
--port=":12342" \
--grpc-gateway-port=":12343" 2> /dev/null &


#################################
# By default, it runs all e2e test cases!
# Use "--ginkgo.skip" to skip tests.
# Use "--ginkgo.focus" to select tests.
echo "running e2e tests"
./tests/e2e/e2e.test \
--ginkgo.v \
--network-runner-log-level error \
--network-runner-grpc-endpoint="0.0.0.0:12342" \
--avalanchego-path=${AVALANCHEGO_PATH} \
--avalanchego-plugin-dir=${AVALANCHEGO_PLUGIN_DIR} \
--avalanchego-log-level=${AVALANCHE_LOG_LEVEL} \
--output-path=/tmp/avalanchego-v${VERSION}/output.yaml \
--mode=${MODE}

EXIT_CODE=$?

sleep 5

if [[ ${EXIT_CODE} -gt 0 ]]; then
  echo "FAILURE with exit code ${EXIT_CODE}"
  exit ${EXIT_CODE}
else
  echo "ALL SUCCESS!"
fi
