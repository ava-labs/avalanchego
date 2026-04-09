#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
NETWORK_DIR="${TMPNET_NETWORK_DIR:-$HOME/.tmpnet/networks/latest}"
OUTPUT="${SCRIPT_DIR}/tx-frontend/public/nodes.json"
CHAINS_FILE="${SCRIPT_DIR}/tx-frontend/public/chains.json"
FUND_AMOUNT="100" # AVAX per node

if [ ! -f "$OUTPUT" ]; then
  echo "Run sync_nodes.sh first to generate nodes.json"
  exit 1
fi

# Build the list of EVM RPC endpoints to fund on
# Always include C-Chain, plus any EVM chains from chains.json
FIRST_URI=$(python3 -c "import json; entries=json.load(open('${OUTPUT}')); uri=entries[0] if isinstance(entries[0],str) else entries[0]['uri']; print(uri)")

RPC_URLS="${FIRST_URI}/ext/bc/C/rpc"

if [ -f "$CHAINS_FILE" ]; then
  # Add all EVM-compatible chains (not platformvm)
  EXTRA_RPCS=$(python3 -c "
import json
chains = json.load(open('${CHAINS_FILE}'))
for c in chains:
    if c.get('vm') not in ('platformvm',) and c.get('chainId') != 'C':
        print(c['rpcPath'])
")
  for rpc_path in $EXTRA_RPCS; do
    RPC_URLS="${RPC_URLS}|${FIRST_URI}${rpc_path}"
  done
fi

echo "Funding $FUND_AMOUNT AVAX per node on all EVM chains..."

export OUTPUT FUND_AMOUNT RPC_URLS
NODE_DATA=$(python3 << 'PYEOF'
import json, hashlib, os

EWOQ_KEY = "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"

output = os.environ["OUTPUT"]
fund_amount = os.environ["FUND_AMOUNT"]
rpc_urls = os.environ["RPC_URLS"].split("|")

with open(output) as f:
    entries = json.load(f)

nodes = []
for i, entry in enumerate(entries):
    uri = entry if isinstance(entry, str) else entry.get("uri", "")
    seed = f"simplex-node-{i}".encode()
    priv = hashlib.sha256(seed).hexdigest()
    nodes.append({"uri": uri, "privateKey": priv, "index": i})

print(json.dumps({"nodes": nodes, "ewoqKey": EWOQ_KEY, "rpcUrls": rpc_urls, "fundAmount": fund_amount}))
PYEOF
)

FUND_SCRIPT="${SCRIPT_DIR}/tx-frontend/fund.mjs"

cat > "$FUND_SCRIPT" << 'JSEOF'
import { JsonRpcProvider, Wallet, parseEther, formatEther } from "ethers";
import { writeFileSync } from "fs";

const config = JSON.parse(process.argv[2]);
const { nodes, ewoqKey, rpcUrls, fundAmount } = config;

const results = [];

for (const rpcUrl of rpcUrls) {
  console.log(`\n=== Funding on ${rpcUrl} ===`);

  const provider = new JsonRpcProvider(rpcUrl);
  const ewoqWallet = new Wallet(ewoqKey, provider);

  try {
    const ewoqBal = await provider.getBalance(ewoqWallet.address);
    console.log(`Ewoq balance: ${formatEther(ewoqBal)} AVAX`);
  } catch (e) {
    console.log(`Skipping ${rpcUrl}: ${e.message}`);
    continue;
  }

  let nonce = await provider.getTransactionCount(ewoqWallet.address);

  for (const node of nodes) {
    const wallet = new Wallet(node.privateKey, provider);
    const address = wallet.address;

    const currentBal = await provider.getBalance(address);
    const currentBalEth = parseFloat(formatEther(currentBal));

    if (currentBalEth < parseFloat(fundAmount) * 0.9) {
      const needed = parseFloat(fundAmount) - currentBalEth;
      console.log(`  Node ${node.index}: ${address} — funding ${needed.toFixed(2)} AVAX...`);
      const tx = await ewoqWallet.sendTransaction({
        to: address,
        value: parseEther(needed.toFixed(18)),
        nonce: nonce++,
      });
      await tx.wait();
      console.log(`    tx: ${tx.hash}`);
    } else {
      console.log(`  Node ${node.index}: ${address} — already funded (${currentBalEth.toFixed(2)} AVAX)`);
    }

    // Only build results from the first RPC (C-Chain)
    if (rpcUrl === rpcUrls[0]) {
      results.push({ uri: node.uri, address });
    }
  }
}

const outputPath = process.argv[3];
writeFileSync(outputPath, JSON.stringify(results, null, 2));
console.log(`\nWrote ${results.length} nodes with addresses to ${outputPath}`);
JSEOF

cd "${SCRIPT_DIR}/tx-frontend"
node fund.mjs "$NODE_DATA" "$OUTPUT"
rm -f "$FUND_SCRIPT"

echo "Done!"
