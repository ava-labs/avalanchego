#!/usr/bin/env bash

set -euo pipefail

# Run the prebuilt simulator against the endpoints in RPC_ENDPOINTS.
: "${SIMULATOR_PATH:?SIMULATOR_PATH must point to a prebuilt simulator}"

exec "${SIMULATOR_PATH}" \
    --endpoints="${RPC_ENDPOINTS}" \
    --key-dir=./cmd/simulator/.simulator/keys \
    --timeout="${SIMULATOR_TIMEOUT:-300s}" \
    --workers=1 \
    --txs-per-worker=50000 \
    --batch-size=50000 \
    --max-fee-cap=1000000 \
    --max-tip-cap=10000
