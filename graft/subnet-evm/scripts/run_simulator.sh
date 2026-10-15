#!/usr/bin/env bash
# This script runs a 30s load simulation using RPC_ENDPOINTS environment variable to specify
# which RPC endpoints to hit.

set -e

echo "Beginning simulator script"

if ! [[ "$0" =~ scripts/run_simulator.sh ]]; then
  echo "must be run from repository root, but got $0"
  exit 255
fi

SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)
# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

run_simulator() {
    #################################
    if [[ -z "${SIMULATOR_PATH:-}" ]]; then
        echo "building simulator"
        pushd ./cmd/simulator
        go build -o ./simulator main/*.go
        echo
        popd
        SIMULATOR_PATH=./cmd/simulator/simulator
    fi

    echo "running simulator from $PWD"
    "${SIMULATOR_PATH}" \
        --endpoints="$RPC_ENDPOINTS" \
        --key-dir=./cmd/simulator/.simulator/keys \
        --timeout=300s \
        --workers=1 \
        --txs-per-worker=50000 \
        --batch-size=50000 \
        --max-fee-cap=1000000 \
        --max-tip-cap=10000
}

run_simulator
