#!/usr/bin/env bash

set -euo pipefail

NETWORK_DIR="${TMPNET_NETWORK_DIR:-$HOME/.tmpnet/networks/latest}"

echo "Checking nodes in ${NETWORK_DIR}..."
echo

for node_dir in "${NETWORK_DIR}"/NodeID-*; do
  [ -d "$node_dir" ] || continue
  node_id=$(basename "$node_dir")
  process_file="${node_dir}/process.json"

  if [ ! -f "$process_file" ]; then
    echo "${node_id}: no process.json found"
    continue
  fi

  uri=$(python3 -c "import json; print(json.load(open('${process_file}'))['uri'])" 2>/dev/null || true)
  if [ -z "$uri" ]; then
    echo "${node_id}: could not read URI"
    continue
  fi

  echo "${node_id} (${uri}):"
  curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}' -H 'content-type:application/json' "${uri}/ext/info" | python3 -m json.tool
  echo
done
