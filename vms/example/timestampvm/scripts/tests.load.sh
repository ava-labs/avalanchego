#!/usr/bin/env bash
# (c) 2019-2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -e

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"

# Ensure we are in the right location
if ! [[ "$0" =~ scripts/tests.load.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# TimestampVM root directory
TIMESTAMPVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)
TERMINAL_HEIGHT=${TERMINAL_HEIGHT:-'1000000'}

# Load the versions
source "$TIMESTAMPVM_PATH"/scripts/versions.sh

# PWD is used in the avalanchego build script so we use a different var
PPWD=$(pwd)
############################
echo "building avalanchego"
ROOT_PATH=/tmp/timestampvm-load
rm -rf ${ROOT_PATH}
mkdir ${ROOT_PATH}
cd ${ROOT_PATH}
git clone https://github.com/ava-labs/avalanchego.git
cd avalanchego
git checkout ${avalanche_version}
# We build AvalancheGo manually instead of downloading binaries
# because the machine code will be better optimized for the local environment
#
# For example, using the pre-built binary on Apple Silicon is about ~40% slower
# because it requires Rosetta 2 emulation.
./scripts/build.sh
cd ${PPWD}

############################
echo "building timestampvm"
BUILD_PATH=${ROOT_PATH}/avalanchego/build
PLUGINS_PATH=${BUILD_PATH}/plugins

# previous binary already deleted in last build phase
go build \
  -o ${PLUGINS_PATH}/tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH \
  ./main/

############################
echo "creating genesis file"
echo -n "e2e" >${ROOT_PATH}/.genesis

############################
echo "creating vm config"
echo -n "{}" >${ROOT_PATH}/.config

############################
echo "creating subnet config"
rm -f /tmp/.subnet
cat <<EOF >${ROOT_PATH}/.subnet
{
  "proposerMinBlockDelay":0
}
EOF

############################
echo "building load.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/load

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
  --log-level warn \
  --port=":12342" \
  --disable-grpc-gateway &
PID=${!}

############################
# By default, it runs all e2e test cases!
# Use "--ginkgo.skip" to skip tests.
# Use "--ginkgo.focus" to select tests.
echo "running load tests"
./tests/load/load.test \
  --ginkgo.v \
  --network-runner-log-level warn \
  --network-runner-grpc-endpoint="0.0.0.0:12342" \
  --avalanchego-path=${BUILD_PATH}/avalanchego \
  --avalanchego-plugin-dir=${PLUGINS_PATH} \
  --vm-genesis-path=${ROOT_PATH}/.genesis \
  --vm-config-path=${ROOT_PATH}/.config \
  --subnet-config-path=${ROOT_PATH}/.subnet \
  --terminal-height=${TERMINAL_HEIGHT}

############################
# load.test" already terminates the cluster
# just in case load tests are aborted, manually terminate them again
echo "network-runner RPC server was running on PID ${PID} as test mode; terminating the process..."
pkill -P ${PID} || true
kill -2 ${PID} || true
pkill -9 -f tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH || true # in case pkill didn't work
