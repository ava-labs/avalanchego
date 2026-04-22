#!/bin/bash
# Extracts useful info from the latest tmpnet for testing multipartystakeserver
set -e

NETWORK_DIR="${TMPNET_NETWORK_DIR:-$HOME/.tmpnet/networks/latest}"

if [ ! -f "$NETWORK_DIR/config.json" ]; then
  echo "ERROR: No tmpnet found at $NETWORK_DIR"
  echo "Start one with: ./build/tmpnetctl start-network --avalanchego-path=./build/avalanchego"
  exit 1
fi

echo "=== tmpnet Info ==="
echo "Network dir: $NETWORK_DIR"
echo ""

# Get first node's API URI
NODE_DIR=$(ls -d "$NETWORK_DIR"/NodeID-* 2>/dev/null | head -1)
if [ -z "$NODE_DIR" ]; then
  echo "ERROR: No nodes found"
  exit 1
fi

NODE_ID=$(basename "$NODE_DIR")
echo "First node: $NODE_ID"

# Parse the node's flags to get the HTTP port
FLAGS_FILE="$NODE_DIR/flags.json"
if [ -f "$FLAGS_FILE" ]; then
  HTTP_PORT=$(python3 -c "import json; f=json.load(open('$FLAGS_FILE')); print(f.get('http-port', 'unknown'))")
  echo "Node URI: http://localhost:$HTTP_PORT"
else
  echo "WARNING: No flags.json found for $NODE_ID"
fi

echo ""
echo "=== Pre-funded Keys (first 3 of 50) ==="
python3 -c "
import json
with open('$NETWORK_DIR/config.json') as f:
    cfg = json.load(f)
for i, k in enumerate(cfg['preFundedKeys'][:3]):
    print(f'  Key {i+1}: {k}')
"

echo ""
echo "=== Genesis Validator BLS Keys ==="
python3 -c "
import json
with open('$NETWORK_DIR/genesis.json') as f:
    gen = json.load(f)
stakers = gen.get('initialStakers', [])
for s in stakers:
    print(f'  NodeID: {s[\"nodeID\"]}')
    print(f'  BLS PubKey: {s[\"signer\"][\"publicKey\"]}')
    print(f'  BLS PoP: {s[\"signer\"][\"proofOfPossession\"]}')
    print()
"

echo "=== Usage ==="
echo "1. Start server:"
echo "   go run ./cmd/multipartystakeserver/ --uri http://localhost:$HTTP_PORT"
echo ""
echo "2. Start frontend:"
echo "   cd cmd/multipartystakeserver/web && npm run dev"
echo ""
echo "3. Open http://localhost:5173"
echo "4. Use 'Private Key (Dev)' mode with keys above"
