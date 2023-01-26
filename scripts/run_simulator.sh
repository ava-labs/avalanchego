#!/usr/bin/env bash
set -e

echo "Beginning simulator script"

if ! [[ "$0" =~ scripts/run_simulator.sh ]]; then
  echo "must be run from repository root, but got $0"
  exit 255
fi

# Load the versions
SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)
source "$SUBNET_EVM_PATH"/scripts/versions.sh

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

run_simulator() {
    #################################
    echo "building simulator"
    pushd ./cmd/simulator
    go install -v .
    echo 

    popd
    echo "running simulator from $PWD"
    simulator \
        --rpc-endpoints=$RPC_ENDPOINTS \
        --keys=./cmd/simulator/.simulator/keys \
        --timeout=30s \
        --concurrency=10 \
        --base-fee=300 \
        --priority-fee=100
}

run_simulator
