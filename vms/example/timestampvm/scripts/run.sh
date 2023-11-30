#!/usr/bin/env bash
# (c) 2019-2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -e

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"

# e.g.,
# ./scripts/run.sh 1.7.13
#
# run without e2e tests
# ./scripts/run.sh 1.7.13
#
# to run E2E tests (terminates cluster afterwards)
# E2E=true ./scripts/run.sh 1.7.13
if ! [[ "$0" =~ scripts/run.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# TimestampVM root directory
TIMESTAMPVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Load the versions
source "$TIMESTAMPVM_PATH"/scripts/versions.sh

MODE=${MODE:-run}
E2E=${E2E:-false}
if [[ ${E2E} == true ]]; then
  MODE="test"
fi

AVALANCHE_LOG_LEVEL=${AVALANCHE_LOG_LEVEL:-INFO}

echo "Running with:"
echo avalanche_version: ${avalanche_version}
echo MODE: ${MODE}

############################
# download avalanchego
# https://github.com/ava-labs/avalanchego/releases
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
AVALANCHEGO_PATH=/tmp/avalanchego-${avalanche_version}/avalanchego
AVALANCHEGO_PLUGIN_DIR=/tmp/avalanchego-${avalanche_version}/plugins

if [ ! -f "$AVALANCHEGO_PATH" ]; then
  DOWNLOAD_URL=https://github.com/ava-labs/avalanchego/releases/download/${avalanche_version}/avalanchego-linux-${GOARCH}-${avalanche_version}.tar.gz
  DOWNLOAD_PATH=/tmp/avalanchego.tar.gz
  if [[ ${GOOS} == "darwin" ]]; then
    DOWNLOAD_URL=https://github.com/ava-labs/avalanchego/releases/download/${avalanche_version}/avalanchego-macos-${avalanche_version}.zip
    DOWNLOAD_PATH=/tmp/avalanchego.zip
  fi

  rm -rf /tmp/avalanchego-${avalanche_version}
  rm -rf /tmp/avalanchego-build
  rm -f ${DOWNLOAD_PATH}

  echo "downloading avalanchego ${avalanche_version} at ${DOWNLOAD_URL}"
  curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

  echo "extracting downloaded avalanchego"
  if [[ ${GOOS} == "linux" ]]; then
    tar xzvf ${DOWNLOAD_PATH} -C /tmp
  elif [[ ${GOOS} == "darwin" ]]; then
    unzip ${DOWNLOAD_PATH} -d /tmp/avalanchego-build
    mv /tmp/avalanchego-build/build /tmp/avalanchego-${avalanche_version}
  fi
  find /tmp/avalanchego-${avalanche_version}
fi

############################

############################
echo "building timestampvm"

# delete previous (if exists)
rm -f /tmp/avalanchego-${avalanche_version}/plugins/tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH

go build \
  -o /tmp/avalanchego-${avalanche_version}/plugins/tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH \
  ./main/
find /tmp/avalanchego-${avalanche_version}

############################

############################

echo "creating genesis file"
echo -n "e2e" >/tmp/.genesis

############################

############################

echo "creating vm config"
echo -n "{}" >/tmp/.config

############################

############################
echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/e2e

#################################
# download avalanche-network-runner
# https://github.com/ava-labs/avalanche-network-runner
ANR_REPO_PATH=github.com/ava-labs/avalanche-network-runner
ANR_VERSION=$avalanche_network_runner_version
# version set
go install -v ${ANR_REPO_PATH}@${ANR_VERSION}

#################################
# run "avalanche-network-runner" server
GOPATH=$(go env GOPATH)
if [[ -z ${GOBIN+x} ]]; then
  # no gobin set
  BIN=${GOPATH}/bin/avalanche-network-runner
else
  # gobin set
  BIN=${GOBIN}/avalanche-network-runner
fi

echo "launch avalanche-network-runner in the background"
$BIN server \
  --log-level debug \
  --port=":12342" \
  --disable-grpc-gateway &
PID=${!}

############################
# By default, it runs all e2e test cases!
# Use "--ginkgo.skip" to skip tests.
# Use "--ginkgo.focus" to select tests.
echo "running e2e tests"
./tests/e2e/e2e.test \
  --ginkgo.v \
  --network-runner-log-level info \
  --network-runner-grpc-endpoint="0.0.0.0:12342" \
  --avalanchego-path=${AVALANCHEGO_PATH} \
  --avalanchego-plugin-dir=${AVALANCHEGO_PLUGIN_DIR} \
  --vm-genesis-path=/tmp/.genesis \
  --vm-config-path=/tmp/.config \
  --output-path=/tmp/avalanchego-${avalanche_version}/output.yaml \
  --mode=${MODE}
STATUS=$?

############################
if [[ -f "/tmp/avalanchego-${avalanche_version}/output.yaml" ]]; then
  echo "cluster is ready!"
  cat /tmp/avalanchego-${avalanche_version}/output.yaml
else
  echo "cluster is not ready in time... terminating ${PID}"
  kill ${PID}
  exit 255
fi

############################
if [[ ${MODE} == "test" ]]; then
  # "e2e.test" already terminates the cluster for "test" mode
  # just in case tests are aborted, manually terminate them again
  echo "network-runner RPC server was running on PID ${PID} as test mode; terminating the process..."
  pkill -P ${PID} || true
  kill -2 ${PID} || true
  pkill -9 -f tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH || true # in case pkill didn't work
  exit ${STATUS}
else
  echo "network-runner RPC server is running on PID ${PID}..."
  echo ""
  echo "use the following command to terminate:"
  echo ""
  echo "pkill -P ${PID} && kill -2 ${PID} && pkill -9 -f tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"
  echo ""
fi
