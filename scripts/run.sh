#!/usr/bin/env bash
set -e

if ! [[ "$0" =~ scripts/run.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

SUBNET_EVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

VERSION=$1
if [[ -z "${VERSION}" ]]; then
  echo "Missing version argument!"
  echo "Usage: ${0} [VERSION]" >> /dev/stderr
  exit 255
fi

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
rm -rf /tmp/avalanchego-build
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
    "D23cbfA7eA985213aD81223309f588A7E66A246A": {
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
#     "D23cbfA7eA985213aD81223309f588A7E66A246A": {
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

echo "building runner"
pushd ./runner
go build -v -o /tmp/runner .
popd

echo "launch local test cluster in the background"
/tmp/runner \
--avalanchego-path=/tmp/avalanchego-v${VERSION}/avalanchego \
--vm-id=srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy \
--vm-genesis-path=/tmp/genesis.json \
--output-path=/tmp/avalanchego-v${VERSION}/output.yaml 2> /dev/null &
PID=${!}

sleep 30
while [[ ! -s /tmp/avalanchego-v${VERSION}/output.yaml ]]; do
  echo "waiting for local cluster on PID ${PID}"
  sleep 5
  # wait up to 5-minute
  ((c++)) && ((c==60)) && break
done

if [[ -f "/tmp/avalanchego-v${VERSION}/output.yaml" ]]; then
  echo "cluster is ready!"
  go run scripts/parser/parse_output.go /tmp/avalanchego-v${VERSION}/output.yaml $CHAIN_ID
else
  echo "cluster is not ready in time... terminating ${PID}"
  kill ${PID}
  exit 255
fi
