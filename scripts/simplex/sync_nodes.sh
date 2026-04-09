#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
NETWORK_DIR="${TMPNET_NETWORK_DIR:-$HOME/.tmpnet/networks/latest}"
OUTPUT="${SCRIPT_DIR}/tx-frontend/public/nodes.json"

uris=()
for node_dir in "${NETWORK_DIR}"/NodeID-*; do
  [ -d "$node_dir" ] || continue
  process_file="${node_dir}/process.json"
  [ -f "$process_file" ] || continue

  uri=$(python3 -c "import json; print(json.load(open('${process_file}'))['uri'])" 2>/dev/null || true)
  [ -n "$uri" ] && uris+=("\"${uri}\"")
done

# Write JSON array
printf '[%s]\n' "$(IFS=,; echo "${uris[*]}")" > "$OUTPUT"

echo "Wrote ${#uris[@]} node URIs to ${OUTPUT}"
