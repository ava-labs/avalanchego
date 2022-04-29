#!/usr/bin/env bash
set -e

# e.g.,
# ./scripts/run.sh 1.7.10 test 0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC
# ./scripts/run.sh 1.7.10 run 0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC
if ! [[ "$0" =~ scripts/run.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

VERSION=$1
if [[ -z "${VERSION}" ]]; then
  echo "Missing version argument!"
  echo "Usage: ${0} [VERSION] [MODE] [GENESIS_ADDRESS]" >> /dev/stderr
  exit 255
fi

MODE=$2
if [[ -z "${MODE}" ]]; then
  echo "Missing mode argument!"
  echo "Usage: ${0} [VERSION] [MODE] [GENESIS_ADDRESS]" >> /dev/stderr
  exit 255
fi

GENESIS_ADDRESS=$3
if [[ -z "${GENESIS_ADDRESS}" ]]; then
  echo "Missing address argument!"
  echo "Usage: ${0} [VERSION] [MODE] [GENESIS_ADDRESS]" >> /dev/stderr
  exit 255
fi

echo "Running e2e tests with:"
echo VERSION: ${VERSION}
echo MODE: ${MODE}
echo GENESIS_ADDRESS: ${GENESIS_ADDRESS}

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
# write subnet-evm genesis

# Create genesis file to use in network (make sure to add your address to
# "alloc")
export CHAIN_ID=99999
echo "creating genesis"
cat <<EOF > /tmp/genesis.json
{
  "config": {
    "chainId": $CHAIN_ID,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip150Hash": "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0",
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "subnetEVMTimestamp": 0,
    "feeConfig": {
      "gasLimit": 20000000,
      "minBaseFee": 1000000000,
      "targetGas": 100000000,
      "baseFeeChangeDenominator": 48,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 10000000,
      "targetBlockRate": 2,
      "blockGasCostStep": 500000
    }
  },
  "alloc": {
    "${GENESIS_ADDRESS:2}": {
      "balance": "0x52B7D2DCC80CD2E4000000"
    }
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x00",
  "gasLimit": "0x1312D00",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
EOF

# If you'd like to try the airdrop feature, use the commented genesis
# cat <<EOF > /tmp/genesis.json
# {
#   "config": {
#     "chainId": $CHAIN_ID,
#     "homesteadBlock": 0,
#     "eip150Block": 0,
#     "eip150Hash": "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0",
#     "eip155Block": 0,
#     "eip158Block": 0,
#     "byzantiumBlock": 0,
#     "constantinopleBlock": 0,
#     "petersburgBlock": 0,
#     "istanbulBlock": 0,
#     "muirGlacierBlock": 0,
#     "subnetEVMTimestamp": 0,
#     "feeConfig": {
#       "gasLimit": 20000000,
#       "minBaseFee": 1000000000,
#       "targetGas": 100000000,
#       "baseFeeChangeDenominator": 48,
#       "minBlockGasCost": 0,
#       "maxBlockGasCost": 10000000,
#       "targetBlockRate": 2,
#       "blockGasCostStep": 500000
#     }
#   },
#   "airdropHash":"0xccbf8e430b30d08b5b3342208781c40b373d1b5885c1903828f367230a2568da",
#   "airdropAmount":"0x8AC7230489E80000",
#   "alloc": {
#     "${GENESIS_ADDRESS:2}": {
#       "balance": "0x52B7D2DCC80CD2E4000000"
#     }
#   },
#   "nonce": "0x0",
#   "timestamp": "0x0",
#   "extraData": "0x00",
#   "gasLimit": "0x1312D00",
#   "difficulty": "0x0",
#   "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
#   "coinbase": "0x0000000000000000000000000000000000000000",
#   "number": "0x0",
#   "gasUsed": "0x0",
#   "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
# }
# EOF

#################################
# download avalanche-network-runner
# https://github.com/ava-labs/avalanche-network-runner
# TODO: use "go install -v github.com/ava-labs/avalanche-network-runner/cmd/avalanche-network-runner@v${NETWORK_RUNNER_VERSION}"
NETWORK_RUNNER_VERSION=1.0.12
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
/tmp/avalanche-network-runner -h

#################################
echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.3
ACK_GINKGO_RC=true ginkgo build ./tests/e2e
./tests/e2e/e2e.test --help

#################################
# run "avalanche-network-runner" server
echo "launch avalanche-network-runner in the background"
/tmp/avalanche-network-runner \
server \
--log-level debug \
--port=":12342" \
--grpc-gateway-port=":12343" &
PID=${!}

#################################
# By default, it runs all e2e test cases!
# Use "--ginkgo.skip" to skip tests.
# Use "--ginkgo.focus" to select tests.
echo "running e2e tests"
./tests/e2e/e2e.test \
--ginkgo.v \
--network-runner-log-level debug \
--network-runner-grpc-endpoint="0.0.0.0:12342" \
--avalanchego-path=${AVALANCHEGO_PATH} \
--avalanchego-plugin-dir=${AVALANCHEGO_PLUGIN_DIR} \
--avalanchego-log-level="DEBUG" \
--vm-genesis-path=/tmp/genesis.json \
--output-path=/tmp/avalanchego-v${VERSION}/output.yaml \
--mode=${MODE}

#################################
# e.g., print out MetaMask endpoints
if [[ -f "/tmp/avalanchego-v${VERSION}/output.yaml" ]]; then
  echo "cluster is ready!"
  go run scripts/parser/main.go /tmp/avalanchego-v${VERSION}/output.yaml $CHAIN_ID $GENESIS_ADDRESS
else
  echo "cluster is not ready in time... terminating ${PID}"
  kill ${PID}
  exit 255
fi

#################################
if [[ ${MODE} == "test" ]]; then
  kill -9 ${PID}
else
  echo "network-runner RPC server is running on PID ${PID}"
fi
echo "ALL SUCCESS!"
