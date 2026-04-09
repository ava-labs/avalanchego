#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
AVALANCHE_PATH=$(cd "${SCRIPT_DIR}/../.." && pwd)
cd "${AVALANCHE_PATH}"

NETWORK_DIR="${TMPNET_NETWORK_DIR:-$HOME/.tmpnet/networks/latest}"
PLUGIN_NAME="srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"

# Build subnet-evm plugin
echo "Building subnet-evm plugin..."
mkdir -p "build/plugins"
go build -o "build/plugins/${PLUGIN_NAME}" ./graft/subnet-evm/plugin/

# Copy plugin to the default plugins dir and each node's plugin dir
echo "Installing plugin to node plugin directories..."
mkdir -p "${HOME}/.avalanchego/plugins"
cp "build/plugins/${PLUGIN_NAME}" "${HOME}/.avalanchego/plugins/"

for node_dir in "${NETWORK_DIR}"/NodeID-*; do
  [ -d "$node_dir" ] || continue
  plugin_dir="${node_dir}/plugins"
  mkdir -p "${plugin_dir}"
  cp "build/plugins/${PLUGIN_NAME}" "${plugin_dir}/"
done

# Create L1 and capture output
echo "Creating L1..."
CREATE_OUTPUT=$(go run ./scripts/simplex/create_l1/ \
  --network-dir="${NETWORK_DIR}" \
  --genesis="${SCRIPT_DIR}/genesis.json" \
  --config="${SCRIPT_DIR}/subnet_config.json" \
  --chains-output="${SCRIPT_DIR}/tx-frontend/public/chains.json" 2>&1)

echo "$CREATE_OUTPUT"

# Parse subnet ID from output
SUBNET_ID=$(echo "$CREATE_OUTPUT" | grep '^SUBNET_ID=' | cut -d= -f2)
if [ -z "$SUBNET_ID" ]; then
  echo "ERROR: Could not parse SUBNET_ID from create_l1 output"
  exit 1
fi

echo ""
echo "Configuring nodes to track subnet ${SUBNET_ID}..."

# Update each node's config.json to track the new subnet
for node_dir in "${NETWORK_DIR}"/NodeID-*; do
  [ -d "$node_dir" ] || continue
  config_file="${node_dir}/config.json"
  [ -f "$config_file" ] || continue

  python3 -c "
import json
with open('${config_file}') as f:
    config = json.load(f)
flags = config.get('flags', {})
existing = flags.get('track-subnets', '')
subnets = set(s for s in existing.split(',') if s)
subnets.add('${SUBNET_ID}')
flags['track-subnets'] = ','.join(subnets)
config['flags'] = flags
with open('${config_file}', 'w') as f:
    json.dump(config, f, indent=2)
"
  echo "  Updated $(basename "$node_dir")"
done

# Restart the network so nodes pick up the new subnet tracking
echo ""
echo "Restarting network..."
./scripts/run_tmpnetctl.sh restart-network --network-dir="${NETWORK_DIR}"

echo ""
echo "Done! Nodes are now tracking subnet ${SUBNET_ID}"
