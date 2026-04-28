#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
AVALANCHE_PATH=$(cd "${SCRIPT_DIR}/../.." && pwd)
cd "${AVALANCHE_PATH}"

NETWORK_DIR="${HOME}/.tmpnet/networks/latest"
if [ -f "${NETWORK_DIR}/network.env" ]; then
  source "${NETWORK_DIR}/network.env"
  NETWORK_DIR="${TMPNET_NETWORK_DIR:-${NETWORK_DIR}}"
fi

SUBNET_CONFIG="${1:-${SCRIPT_DIR}/config/subnet_config_simplex.json}"

echo "Creating L1..."
go run ./scripts/simplex/create_l1/ \
  --network-dir="${NETWORK_DIR}" \
  --genesis="${SCRIPT_DIR}/config/genesis.json" \
  --config="${SUBNET_CONFIG}" \
  --chains-output="${SCRIPT_DIR}/tx-frontend/public/chains.json" \
  --resolved-config-output="${SCRIPT_DIR}/config/.resolved_subnet_config.json"
