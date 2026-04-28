#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

echo "Cleaning up previous network..."

# Stop the network gracefully via tmpnetctl if possible
NETWORK_DIR="${HOME}/.tmpnet/networks/latest"
if [ -f "${NETWORK_DIR}/network.env" ]; then
  source "${NETWORK_DIR}/network.env"
  NETWORK_DIR="${TMPNET_NETWORK_DIR:-${NETWORK_DIR}}"
fi

AVALANCHE_PATH=$(cd "${SCRIPT_DIR}/../.." && pwd)
if [ -f "${AVALANCHE_PATH}/build/tmpnetctl" ]; then
  "${AVALANCHE_PATH}/build/tmpnetctl" stop-network --network-dir="${NETWORK_DIR}" 2>/dev/null || true
fi

# Kill any remaining AvalancheGo processes
if pgrep -f "avalanchego" > /dev/null 2>&1; then
  echo "Killing remaining AvalancheGo processes..."
  pkill -f "avalanchego" 2>/dev/null || true
  sleep 1
fi

# Remove tmpnet network data
if [ -d "${HOME}/.tmpnet/networks" ]; then
  echo "Removing tmpnet network data..."
  rm -rf "${HOME}/.tmpnet/networks"
fi

# Clean generated frontend files
rm -f "${SCRIPT_DIR}/tx-frontend/public/nodes.json"
rm -f "${SCRIPT_DIR}/tx-frontend/public/chains.json"
rm -f "${SCRIPT_DIR}/config/.resolved_subnet_config.json"

echo "Clean complete."
